package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image/color"
	"log"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const gameAreaSizeX, gameAreaSizeY = 547, 540
const fieldCenterX, fieldCenterY = gameAreaSizeX / 2, gameAreaSizeY / 2

var mouseX, mouseY uint16

var gameCtx context.Context
var scale int = 3

// drawState tracks information needed by the Ebiten renderer.
type drawState struct {
	descriptors  map[uint8]frameDescriptor
	pictures     []framePicture
	prevPictures []framePicture
	mobiles      map[uint8]frameMobile
	prevMobiles  map[uint8]frameMobile
	prevTime     time.Time
	curTime      time.Time
}

var (
	state = drawState{
		descriptors:  make(map[uint8]frameDescriptor),
		mobiles:      make(map[uint8]frameMobile),
		prevMobiles:  make(map[uint8]frameMobile),
		prevPictures: []framePicture{},
	}
	stateMu sync.Mutex
)

type Game struct{}

// picMatcher helps pair current pictures with those from the previous frame
// when the ordering differs between frames.
type picMatcher struct {
	prevByID map[uint16][]int
	prev     []framePicture
	used     []bool
}

func newPicMatcher(prev []framePicture) *picMatcher {
	pm := &picMatcher{
		prevByID: make(map[uint16][]int),
		prev:     prev,
		used:     make([]bool, len(prev)),
	}
	for i, p := range prev {
		pm.prevByID[p.PictID] = append(pm.prevByID[p.PictID], i)
	}
	return pm
}

func (pm *picMatcher) match(p framePicture) (framePicture, bool) {
	idxs := pm.prevByID[p.PictID]
	bestIdx := -1
	bestDist := int(^uint(0) >> 1) // max int
	for _, i := range idxs {
		if pm.used[i] {
			continue
		}
		prev := pm.prev[i]
		dist := abs(int(prev.H)-int(p.H)) + abs(int(prev.V)-int(p.V))
		if dist < bestDist {
			bestDist = dist
			bestIdx = i
		}
	}
	if bestIdx >= 0 {
		pm.used[bestIdx] = true
		return pm.prev[bestIdx], true
	}
	return framePicture{}, false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (g *Game) Update() error {
	select {
	case <-gameCtx.Done():
		return fmt.Errorf("context done")
	default:
	}
	x, y := ebiten.CursorPosition()
	mouseX = uint16(x)
	mouseY = uint16(y)
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0x00, 0x00, 0x00, 0xff})

	stateMu.Lock()
	descs := make([]frameDescriptor, 0, len(state.descriptors))
	descMap := make(map[uint8]frameDescriptor, len(state.descriptors))
	for idx, d := range state.descriptors {
		descs = append(descs, d)
		descMap[idx] = d
	}
	pics := append([]framePicture(nil), state.pictures...)
	prevPics := append([]framePicture(nil), state.prevPictures...)
	matcher := newPicMatcher(prevPics)
	mobiles := make([]frameMobile, 0, len(state.mobiles))
	for _, m := range state.mobiles {
		mobiles = append(mobiles, m)
	}
	prevMobiles := make(map[uint8]frameMobile, len(state.prevMobiles))
	for idx, m := range state.prevMobiles {
		prevMobiles[idx] = m
	}
	prevTime := state.prevTime
	curTime := state.curTime
	stateMu.Unlock()

	alpha := 1.0
	if !curTime.IsZero() && curTime.After(prevTime) {
		elapsed := time.Since(prevTime)
		interval := curTime.Sub(prevTime)
		alpha = float64(elapsed) / float64(interval)
		if alpha < 0 {
			alpha = 0
		}
		if alpha > 1 {
			alpha = 1
		}
	}

	type picItem struct {
		pic framePicture
		idx int
	}
	picItems := make([]picItem, len(pics))
	for i, p := range pics {
		picItems[i] = picItem{pic: p, idx: i}
	}
	sort.Slice(picItems, func(i, j int) bool {
		pi := 0
		pj := 0
		if clImages != nil {
			pi = clImages.Plane(uint32(picItems[i].pic.PictID))
			pj = clImages.Plane(uint32(picItems[j].pic.PictID))
		}
		if pi == pj {
			return picItems[i].pic.V < picItems[j].pic.V
		}
		return pi < pj
	})

	dead := make([]frameMobile, 0, len(mobiles))
	live := make([]frameMobile, 0, len(mobiles))
	for _, m := range mobiles {
		if m.State == poseDead {
			dead = append(dead, m)
		}
		live = append(live, m)
	}

	type textItem struct {
		x, y int
		txt  string
	}
	texts := []textItem{}

	drawMobile := func(m frameMobile) {
		h := float64(m.H)
		v := float64(m.V)
		if pm, ok := prevMobiles[m.Index]; ok {
			if pm.H != m.H || pm.V != m.V {
				h = float64(pm.H)*(1-alpha) + float64(m.H)*alpha
				v = float64(pm.V)*(1-alpha) + float64(m.V)*alpha
			}
		}
		x := int(h) + fieldCenterX
		y := int(v) + fieldCenterY
		var img *ebiten.Image
		if d, ok := descMap[m.Index]; ok {
			img = loadMobileFrame(d.PictID, m.State)
		}
		if img != nil {
			size := img.Bounds().Dx()
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(x-size/2), float64(y-size/2))
			screen.DrawImage(img, op)
		} else {
			ebitenutil.DrawRect(screen, float64(x)-3, float64(y)-3, 6, 6, color.RGBA{0xff, 0, 0, 0xff})
		}
		texts = append(texts, textItem{x + 6, y - 8, fmt.Sprintf("%d", m.Index)})
	}

	drawPicture := func(p framePicture, idx int) {
		h := float64(p.H)
		v := float64(p.V)
		if prev, ok := matcher.match(p); ok {
			h = float64(prev.H)*(1-alpha) + float64(p.H)*alpha
			v = float64(prev.V)*(1-alpha) + float64(p.V)*alpha
		} else if idx < len(prevPics) {
			ph := prevPics[idx].H
			pv := prevPics[idx].V
			if ph != p.H || pv != p.V {
				h = float64(ph)*(1-alpha) + float64(p.H)*alpha
				v = float64(pv)*(1-alpha) + float64(p.V)*alpha
			}
		}
		x := int(h) + fieldCenterX
		y := int(v) + fieldCenterY
		if img := loadImage(p.PictID); img != nil {
			op := &ebiten.DrawImageOptions{}
			w, h := img.Bounds().Dx(), img.Bounds().Dy()
			op.GeoM.Translate(float64(x-w/2), float64(y-h/2))
			screen.DrawImage(img, op)
		} else {
			ebitenutil.DrawRect(screen, float64(x)-2, float64(y)-2, 4, 4, color.RGBA{0, 0, 0xff, 0xff})
		}
		texts = append(texts, textItem{x + 4, y - 8, fmt.Sprintf("%d", p.PictID)})
	}

	// sort pictures by plane and split them into negative, zero and positive planes
	negPics := make([]picItem, 0)
	zeroPics := make([]picItem, 0)
	posPics := make([]picItem, 0)
	for _, it := range picItems {
		p := it.pic
		plane := 0
		if clImages != nil {
			plane = clImages.Plane(uint32(p.PictID))
		}
		switch {
		case plane < 0:
			negPics = append(negPics, it)
		case plane == 0:
			zeroPics = append(zeroPics, it)
		default:
			posPics = append(posPics, it)
		}
	}

	// draw pictures below mobiles
	for _, it := range negPics {
		drawPicture(it.pic, it.idx)
	}

	// draw fallen mobiles before merging
	sort.Slice(dead, func(i, j int) bool { return dead[i].V < dead[j].V })
	for _, m := range dead {
		drawMobile(m)
	}

	sort.Slice(live, func(i, j int) bool { return live[i].V < live[j].V })

	// merge plane 0 pictures with living mobiles by vertical coordinate
	i, j := 0, 0
	for i < len(live) || j < len(zeroPics) {
		var mV, pV int
		if i < len(live) {
			mV = int(live[i].V)
		} else {
			mV = int(^uint(0) >> 1) // max int
		}
		if j < len(zeroPics) {
			pV = int(zeroPics[j].pic.V)
		} else {
			pV = int(^uint(0) >> 1)
		}
		if mV < pV {
			if live[i].State != poseDead {
				drawMobile(live[i])
			}
			i++
		} else {
			drawPicture(zeroPics[j].pic, zeroPics[j].idx)
			j++
		}
	}

	for _, it := range posPics {
		drawPicture(it.pic, it.idx)
	}

	/*
		for _, t := range texts {
			ebitenutil.DebugPrintAt(screen, t.txt, t.x, t.y)
		}
	*/

	lines := make([]string, 0, len(descs))
	for _, d := range descs {
		lines = append(lines, fmt.Sprintf("%d:%s id=%d t=%d", d.Index, d.Name, d.PictID, d.Type))
	}
	//ebitenutil.DebugPrintAt(screen, strings.Join(lines, "\n"), 4, 4)
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("desc:%d pict:%d mobile:%d", len(descs), len(pics), len(mobiles)), 490, 460)

	msgs := getMessages()
	startY := 480 - 12*len(msgs) - 6
	for i, msg := range msgs {
		ebitenutil.DebugPrintAt(screen, msg, 4, startY+12*i)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return gameAreaSizeX, gameAreaSizeY
}

func runGame(ctx context.Context) {
	gameCtx = ctx
	ebiten.SetWindowSize(gameAreaSizeX*scale, gameAreaSizeY*scale)
	ebiten.SetWindowTitle("Draw State")
	if err := ebiten.RunGame(&Game{}); err != nil {
		log.Printf("ebiten: %v", err)
	}
}

func sendInputLoop(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if err := sendPlayerInput(conn); err != nil {
			fmt.Printf("send player input: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func udpReadLoop(ctx context.Context, conn net.Conn) {
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("udp deadline: %v\n", err)
			return
		}
		m, err := readUDPMessage(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			fmt.Printf("udp read error: %v\n", err)
			return
		}
		tag := binary.BigEndian.Uint16(m[:2])
		if tag == 2 { // kMsgDrawState
			handleDrawState(m)
			continue
		}
		if txt := decodeMessage(m); txt != "" {
			fmt.Println(txt)
			addMessage(txt)
		} else {
			fmt.Printf("udp msg tag %d len %d\n", tag, len(m))
		}
	}
}

func tcpReadLoop(ctx context.Context, conn net.Conn) {
loop:
	for {
		if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
			fmt.Printf("set read deadline: %v\n", err)
			break
		}
		m, err := readMessage(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					break loop
				default:
					continue
				}
			}
			fmt.Printf("read error: %v\n", err)
			break
		}
		tag := binary.BigEndian.Uint16(m[:2])
		if tag == 2 { // kMsgDrawState
			handleDrawState(m)
			continue
		}
		if txt := decodeMessage(m); txt != "" {
			fmt.Println(txt)
			addMessage(txt)
		} else {
			fmt.Printf("msg tag %d len %d\n", tag, len(m))
		}
		select {
		case <-ctx.Done():
			break loop
		default:
		}
	}
}

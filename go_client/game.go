package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image/color"
	"log"
	"math"
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
var mouseDown bool

var gameCtx context.Context
var scale int = 3
var interp bool

// drawState tracks information needed by the Ebiten renderer.
type drawState struct {
	descriptors  map[uint8]frameDescriptor
	pictures     []framePicture
	prevPictures []framePicture
	picShiftX    int
	picShiftY    int
	mobiles      map[uint8]frameMobile
	prevMobiles  map[uint8]frameMobile
	prevTime     time.Time
	curTime      time.Time
}

var (
	state = drawState{
		descriptors:  make(map[uint8]frameDescriptor),
		prevPictures: nil,
		mobiles:      make(map[uint8]frameMobile),
		prevMobiles:  make(map[uint8]frameMobile),
	}
	stateMu sync.Mutex
)

type Game struct{}

func (g *Game) Update() error {
	select {
	case <-gameCtx.Done():
		return fmt.Errorf("context done")
	default:
	}
	x, y := ebiten.CursorPosition()
	mouseX = uint16(x)
	mouseY = uint16(y)
	mouseDown = ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
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
	picShiftX := state.picShiftX
	picShiftY := state.picShiftY
	mobiles := make([]frameMobile, 0, len(state.mobiles))
	for _, m := range state.mobiles {
		mobiles = append(mobiles, m)
	}
	var prevMobiles map[uint8]frameMobile
	if interp {
		prevMobiles = make(map[uint8]frameMobile, len(state.prevMobiles))
		for idx, m := range state.prevMobiles {
			prevMobiles[idx] = m
		}
	}
	prevTime := state.prevTime
	curTime := state.curTime
	stateMu.Unlock()

	alpha := 1.0
	if interp && !curTime.IsZero() && curTime.After(prevTime) {
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
	dlog("Draw alpha=%.2f shift=(%d,%d) pics=%d", alpha, picShiftX, picShiftY, len(pics))

	sort.Slice(pics, func(i, j int) bool {
		pi := 0
		pj := 0
		if clImages != nil {
			pi = clImages.Plane(uint32(pics[i].PictID))
			pj = clImages.Plane(uint32(pics[j].PictID))
		}
		if pi == pj {
			return pics[i].V < pics[j].V
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
		if interp {
			if pm, ok := prevMobiles[m.Index]; ok {
				h = float64(pm.H)*(1-alpha) + float64(m.H)*alpha
				v = float64(pm.V)*(1-alpha) + float64(m.V)*alpha
			}
		}
		x := int(math.Round(h)) + fieldCenterX
		y := int(math.Round(v)) + fieldCenterY
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

	drawPicture := func(p framePicture) {
		// pictureShift computes current - previous coordinates, so
		// negate the values to start drawing at the old position and
		// interpolate toward the new one.
		offX := -float64(picShiftX) * (1 - alpha)
		offY := -float64(picShiftY) * (1 - alpha)
		x := int(math.Round(float64(p.H)+offX)) + fieldCenterX
		y := int(math.Round(float64(p.V)+offY)) + fieldCenterY
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
	negPics := make([]framePicture, 0)
	zeroPics := make([]framePicture, 0)
	posPics := make([]framePicture, 0)
	for _, p := range pics {
		plane := 0
		if clImages != nil {
			plane = clImages.Plane(uint32(p.PictID))
		}
		switch {
		case plane < 0:
			negPics = append(negPics, p)
		case plane == 0:
			zeroPics = append(zeroPics, p)
		default:
			posPics = append(posPics, p)
		}
	}

	// draw pictures below mobiles
	for _, p := range negPics {
		drawPicture(p)
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
			pV = int(zeroPics[j].V)
		} else {
			pV = int(^uint(0) >> 1)
		}
		if mV < pV {
			if live[i].State != poseDead {
				drawMobile(live[i])
			}
			i++
		} else {
			drawPicture(zeroPics[j])
			j++
		}
	}

	for _, p := range posPics {
		drawPicture(p)
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

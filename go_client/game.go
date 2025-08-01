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
	"strings"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font/basicfont"
)

const gameAreaSizeX, gameAreaSizeY = 547, 540
const fieldCenterX, fieldCenterY = gameAreaSizeX / 2, gameAreaSizeY / 2
const epsilon = 0.01 // in pixels

var mouseX, mouseY int16
var mouseDown bool

var inputActive bool
var inputText []rune
var inputBg *ebiten.Image
var hudPixel *ebiten.Image

var gameCtx context.Context
var scale int = 3
var interp bool
var onion bool
var linear bool
var drawFilter = ebiten.FilterNearest
var frameCounter int

// drawState tracks information needed by the Ebiten renderer.
type drawState struct {
	descriptors map[uint8]frameDescriptor
	pictures    []framePicture
	picShiftX   int
	picShiftY   int
	mobiles     map[uint8]frameMobile
	prevMobiles map[uint8]frameMobile
	prevDescs   map[uint8]frameDescriptor
	prevTime    time.Time
	curTime     time.Time

	hp, hpMax           int
	sp, spMax           int
	balance, balanceMax int
}

var (
	state = drawState{
		descriptors: make(map[uint8]frameDescriptor),
		mobiles:     make(map[uint8]frameMobile),
		prevMobiles: make(map[uint8]frameMobile),
		prevDescs:   make(map[uint8]frameDescriptor),
	}
	stateMu sync.Mutex
)

// drawSnapshot is a read-only copy of the current draw state.
type drawSnapshot struct {
	descriptors map[uint8]frameDescriptor
	pictures    []framePicture
	picShiftX   int
	picShiftY   int
	mobiles     []frameMobile
	prevMobiles map[uint8]frameMobile
	prevDescs   map[uint8]frameDescriptor
	prevTime    time.Time
	curTime     time.Time
	hp, hpMax   int
	sp, spMax   int
	balance     int
	balanceMax  int
}

// captureDrawSnapshot copies the shared draw state under a mutex.
func captureDrawSnapshot() drawSnapshot {
	stateMu.Lock()
	defer stateMu.Unlock()

	snap := drawSnapshot{
		descriptors: make(map[uint8]frameDescriptor, len(state.descriptors)),
		pictures:    append([]framePicture(nil), state.pictures...),
		picShiftX:   state.picShiftX,
		picShiftY:   state.picShiftY,
		mobiles:     make([]frameMobile, 0, len(state.mobiles)),
		prevTime:    state.prevTime,
		curTime:     state.curTime,
		hp:          state.hp,
		hpMax:       state.hpMax,
		sp:          state.sp,
		spMax:       state.spMax,
		balance:     state.balance,
		balanceMax:  state.balanceMax,
	}

	for idx, d := range state.descriptors {
		snap.descriptors[idx] = d
	}
	for _, m := range state.mobiles {
		snap.mobiles = append(snap.mobiles, m)
	}
	if interp || onion {
		snap.prevMobiles = make(map[uint8]frameMobile, len(state.prevMobiles))
		for idx, m := range state.prevMobiles {
			snap.prevMobiles[idx] = m
		}
	}
	if onion {
		snap.prevDescs = make(map[uint8]frameDescriptor, len(state.prevDescs))
		for idx, d := range state.prevDescs {
			snap.prevDescs[idx] = d
		}
	}
	return snap
}

// computeInterpolation returns the blend factors for frame interpolation and onion skinning.
func computeInterpolation(prevTime, curTime time.Time) (alpha float64, fade float32) {
	alpha = 1.0
	fade = 1.0
	if (interp || onion) && !curTime.IsZero() && curTime.After(prevTime) {
		elapsed := time.Since(prevTime)
		interval := curTime.Sub(prevTime)
		if interp {
			alpha = float64(elapsed) / float64(interval)
			if alpha < 0 {
				alpha = 0
			}
			if alpha > 1 {
				alpha = 1
			}
		}
		if onion {
			half := interval / 2
			if half > 0 {
				fade = float32(float64(elapsed) / float64(half))
			}
			if fade < 0 {
				fade = 0
			}
			if fade > 1 {
				fade = 1
			}
		}
	}
	return alpha, fade
}

type Game struct{}

func (g *Game) Update() error {
	if inputActive {
		inputText = append(inputText, ebiten.AppendInputChars(nil)...)
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
			if len(inputText) > 0 {
				inputText = inputText[:len(inputText)-1]
			}
		} else if d := inpututil.KeyPressDuration(ebiten.KeyBackspace); d > 30 && d%3 == 0 {
			if len(inputText) > 0 {
				inputText = inputText[:len(inputText)-1]
			}
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			txt := strings.TrimSpace(string(inputText))
			if txt != "" {
				sendCommand(txt)
			}
			inputActive = false
			inputText = inputText[:0]
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
			inputActive = false
			inputText = inputText[:0]
		}
	} else {
		if inpututil.IsKeyJustPressed(ebiten.KeyEnter) {
			inputActive = true
			inputText = inputText[:0]
		}
	}

	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	snap := captureDrawSnapshot()
	alpha, fade := computeInterpolation(snap.prevTime, snap.curTime)
	dlog("Draw alpha=%.2f shift=(%d,%d) pics=%d", alpha, snap.picShiftX, snap.picShiftY, len(snap.pictures))
	drawScene(screen, snap, alpha, fade)
	drawNightOverlay(screen)
	drawStatusBars(screen, snap)
	drawMessages(screen, getMessages())
	if inputActive {
		drawInputOverlay(screen, string(inputText))
	}
}

// drawScene renders all world objects for the current frame.
func drawScene(screen *ebiten.Image, snap drawSnapshot, alpha float64, fade float32) {
	descMap := make(map[uint8]frameDescriptor, len(snap.descriptors))
	for idx, d := range snap.descriptors {
		descMap[idx] = d
	}

	sort.Slice(snap.pictures, func(i, j int) bool {
		pi := 0
		pj := 0
		if clImages != nil {
			pi = clImages.Plane(uint32(snap.pictures[i].PictID))
			pj = clImages.Plane(uint32(snap.pictures[j].PictID))
		}
		if pi == pj {
			return snap.pictures[i].V < snap.pictures[j].V
		}
		return pi < pj
	})

	dead := make([]frameMobile, 0, len(snap.mobiles))
	live := make([]frameMobile, 0, len(snap.mobiles))
	for _, m := range snap.mobiles {
		if m.State == poseDead {
			dead = append(dead, m)
		}
		live = append(live, m)
	}

	negPics := make([]framePicture, 0)
	zeroPics := make([]framePicture, 0)
	posPics := make([]framePicture, 0)
	for _, p := range snap.pictures {
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

	for _, p := range negPics {
		drawPicture(screen, p, snap.picShiftX, snap.picShiftY, alpha)
	}

	sort.Slice(dead, func(i, j int) bool { return dead[i].V < dead[j].V })
	for _, m := range dead {
		drawMobile(screen, m, descMap, snap.prevMobiles, snap.prevDescs, alpha, fade)
	}

	sort.Slice(live, func(i, j int) bool { return live[i].V < live[j].V })
	i, j := 0, 0
	for i < len(live) || j < len(zeroPics) {
		var mV, pV int
		if i < len(live) {
			mV = int(live[i].V)
		} else {
			mV = int(^uint(0) >> 1)
		}
		if j < len(zeroPics) {
			pV = int(zeroPics[j].V)
		} else {
			pV = int(^uint(0) >> 1)
		}
		if mV < pV {
			if live[i].State != poseDead {
				drawMobile(screen, live[i], descMap, snap.prevMobiles, snap.prevDescs, alpha, fade)
			}
			i++
		} else {
			drawPicture(screen, zeroPics[j], snap.picShiftX, snap.picShiftY, alpha)
			j++
		}
	}

	for _, p := range posPics {
		drawPicture(screen, p, snap.picShiftX, snap.picShiftY, alpha)
	}
}

// drawMobile renders a single mobile object with optional interpolation and onion skinning.
func drawMobile(screen *ebiten.Image, m frameMobile, descMap map[uint8]frameDescriptor, prevMobiles map[uint8]frameMobile, prevDescs map[uint8]frameDescriptor, alpha float64, fade float32) {
	h := float64(m.H)
	v := float64(m.V)
	if interp {
		if pm, ok := prevMobiles[m.Index]; ok {
			dh := int(m.H) - int(pm.H)
			dv := int(m.V) - int(pm.V)
			if dh*dh+dv*dv <= maxInterpPixels*maxInterpPixels {
				h = float64(pm.H)*(1-alpha) + float64(m.H)*alpha
				v = float64(pm.V)*(1-alpha) + float64(m.V)*alpha
			}
		}
	}
	x := (int(math.Round(h)) + fieldCenterX) * scale
	y := (int(math.Round(v)) + fieldCenterY) * scale
	var img *ebiten.Image
	if d, ok := descMap[m.Index]; ok {
		colors := d.Colors
		if p := getPlayer(d.Name); p != nil && len(p.Colors) > 0 {
			colors = p.Colors
		}
		img = loadMobileFrame(d.PictID, m.State, colors)
	}
	var prevImg *ebiten.Image
	if onion {
		if pm, ok := prevMobiles[m.Index]; ok {
			pd := descMap[m.Index]
			if d, ok := prevDescs[m.Index]; ok {
				pd = d
			}
			prevColors := pd.Colors
			if p := getPlayer(pd.Name); p != nil && len(p.Colors) > 0 {
				prevColors = p.Colors
			}
			prevImg = loadMobileFrame(pd.PictID, pm.State, prevColors)
		}
	}
	if img != nil {
		size := img.Bounds().Dx()
		if onion && prevImg != nil {
			tmp := getTempImage(size)
			off := (tmp.Bounds().Dx() - size) / 2
			op1 := &ebiten.DrawImageOptions{}
			op1.ColorScale.ScaleAlpha(1 - fade)
			op1.Blend = ebiten.BlendCopy
			op1.GeoM.Translate(float64(off), float64(off))
			tmp.DrawImage(prevImg, op1)
			op2 := &ebiten.DrawImageOptions{}
			op2.ColorScale.ScaleAlpha(fade)
			op2.Blend = ebiten.BlendLighter
			op2.GeoM.Translate(float64(off), float64(off))
			tmp.DrawImage(img, op2)
			op := &ebiten.DrawImageOptions{}
			op.Filter = drawFilter
			op.GeoM.Scale(float64(scale), float64(scale))
			op.GeoM.Translate(float64(x-tmp.Bounds().Dx()*scale/2), float64(y-tmp.Bounds().Dy()*scale/2))
			screen.DrawImage(tmp, op)
			recycleTempImage(tmp)
		} else {
			op := &ebiten.DrawImageOptions{}
			op.Filter = drawFilter
			op.GeoM.Scale(float64(scale), float64(scale))
			op.GeoM.Translate(float64(x-size*scale/2), float64(y-size*scale/2))
			screen.DrawImage(img, op)
		}
		if d, ok := descMap[m.Index]; ok && d.Name != "" {
			textClr, bgClr, frameClr := mobileNameColors(m.Colors)
			face := basicfont.Face7x13
			bounds := text.BoundString(face, d.Name)
			w := bounds.Dx()
			h := bounds.Dy()
			top := y + size*scale/2 + 2*scale
			left := x - w/2 - 2
			vector.StrokeRect(screen, float32(left), float32(top), float32(w+4), float32(h+4), 1, frameClr, false)
			ebitenutil.DrawRect(screen, float64(left+1), float64(top+1), float64(w+2), float64(h+2), bgClr)
			text.Draw(screen, d.Name, face, left+2, top+1+h, textClr)
		}
	} else {
		vector.DrawFilledRect(screen, float32(x-3*scale), float32(y-3*scale), float32(6*scale), float32(6*scale), color.RGBA{0xff, 0, 0, 0xff}, false)
	}
}

// drawPicture renders a single picture sprite.
func drawPicture(screen *ebiten.Image, p framePicture, shiftX, shiftY int, alpha float64) {
	offX := -float64(shiftX) * (1 - alpha)
	offY := -float64(shiftY) * (1 - alpha)
	x := (int(math.Round(float64(p.H)+offX)) + fieldCenterX) * scale
	y := (int(math.Round(float64(p.V)+offY)) + fieldCenterY) * scale
	frame := 0
	if clImages != nil {
		frame = clImages.FrameIndex(uint32(p.PictID), frameCounter)
	}
	if img := loadImageFrame(p.PictID, frame); img != nil {
		op := &ebiten.DrawImageOptions{}
		op.Filter = drawFilter
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if linear {
			op.GeoM.Scale(float64(scale)+epsilon, float64(scale)+epsilon)
		} else {
			op.GeoM.Scale(float64(scale), float64(scale))
		}
		op.GeoM.Translate(float64(x-w*scale/2), float64(y-h*scale/2))
		screen.DrawImage(img, op)
	} else {
		vector.DrawFilledRect(screen, float32(x-2*scale), float32(y-2*scale), float32(4*scale), float32(4*scale), color.RGBA{0, 0, 0xff, 0xff}, false)
	}
}

// drawStatusBars renders health, balance and spirit bars.
func drawStatusBars(screen *ebiten.Image, snap drawSnapshot) {
	if hudPixel == nil {
		hudPixel = ebiten.NewImage(1, 1)
		hudPixel.Fill(color.White)
	}
	drawRect := func(x, y, w, h int, clr color.RGBA) {
		op := &ebiten.DrawImageOptions{}
		op.GeoM.Scale(float64(w), float64(h))
		op.GeoM.Translate(float64(x), float64(y))
		op.ColorM.Scale(float64(clr.R)/255, float64(clr.G)/255, float64(clr.B)/255, float64(clr.A)/255)
		screen.DrawImage(hudPixel, op)
	}
	gap := 56 * scale
	barWidth := ((gameAreaSizeX*scale - gap*2) / 3) / 2
	barHeight := 8 * scale
	barY := gameAreaSizeY*scale - barHeight - 2
	totalWidth := 3*barWidth + gap*2
	x := (gameAreaSizeX*scale - totalWidth) / 2
	drawBar := func(x int, cur, max int, clr color.RGBA) {
		frameClr := color.RGBA{0xff, 0xff, 0xff, 0xff}
		vector.StrokeRect(screen, float32(x-scale), float32(barY-scale), float32(barWidth+2*scale), float32(barHeight+2*scale), 1, frameClr, false)
		if max > 0 && cur > 0 {
			w := barWidth * cur / max
			fillClr := color.RGBA{clr.R, clr.G, clr.B, 128}
			drawRect(x, barY, w, barHeight, fillClr)
		}
	}
	drawBar(x, snap.hp, snap.hpMax, color.RGBA{0xff, 0, 0, 0xff})
	x += barWidth + gap
	drawBar(x, snap.balance, snap.balanceMax, color.RGBA{0x00, 0xff, 0x00, 0xff})
	x += barWidth + gap
	drawBar(x, snap.sp, snap.spMax, color.RGBA{0x00, 0x00, 0xff, 0xff})
}

// drawMessages prints chat messages on the HUD.
func drawMessages(screen *ebiten.Image, msgs []string) {
	startY := 480*scale - 12*len(msgs)*scale - 6*scale
	for i, msg := range msgs {
		ebitenutil.DebugPrintAt(screen, msg, 4*scale, startY+12*i*scale)
	}
}

// drawInputOverlay renders the text entry box when chatting.
func drawInputOverlay(screen *ebiten.Image, txt string) {
	if inputBg == nil {
		inputBg = ebiten.NewImage(gameAreaSizeX*scale, 12*scale)
		inputBg.Fill(color.RGBA{0, 0, 0, 128})
	}
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(0, float64(gameAreaSizeY*scale-12*scale))
	screen.DrawImage(inputBg, op)
	ebitenutil.DebugPrintAt(screen, txt, 4*scale, gameAreaSizeY*scale-10*scale)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return gameAreaSizeX * scale, gameAreaSizeY * scale
}

func runGame(ctx context.Context) {
	gameCtx = ctx
	ebiten.SetWindowSize(gameAreaSizeX*scale, gameAreaSizeY*scale)
	ebiten.SetWindowTitle("Draw State")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	if err := ebiten.RunGame(&Game{}); err != nil {
		log.Printf("ebiten: %v", err)
	}
}

func sendInputLoop(ctx context.Context, conn net.Conn) {
	ticker := time.NewTicker(200 * time.Millisecond)
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
		m, err := readTCPMessage(conn)
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

func sendCommand(txt string) {
	if tcpConn != nil {
		if err := sendCommandText(tcpConn, txt); err != nil {
			fmt.Printf("send command: %v\n", err)
		}
	}
}

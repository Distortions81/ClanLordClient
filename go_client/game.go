package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image/color"
	"log"
	"net"
	"sync"
	"time"

	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

var mouseX, mouseY uint16

var gameCtx context.Context

// drawState tracks information needed by the Ebiten renderer.
type drawState struct {
	descriptors map[uint8]frameDescriptor
	pictures    []framePicture
	mobiles     map[uint8]frameMobile
}

var (
	state = drawState{
		descriptors: make(map[uint8]frameDescriptor),
		mobiles:     make(map[uint8]frameMobile),
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
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0xe0, 0xe0, 0xe0, 0xff})

	stateMu.Lock()
	descs := make([]frameDescriptor, 0, len(state.descriptors))
	descMap := make(map[uint8]frameDescriptor, len(state.descriptors))
	for idx, d := range state.descriptors {
		descs = append(descs, d)
		descMap[idx] = d
	}
	pics := append([]framePicture(nil), state.pictures...)
	mobiles := make([]frameMobile, 0, len(state.mobiles))
	for _, m := range state.mobiles {
		mobiles = append(mobiles, m)
	}
	stateMu.Unlock()

	for _, p := range pics {
		x := int(p.H) + 320
		y := int(p.V) + 240
		if img := loadImage(p.PictID); img != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(x), float64(y))
			screen.DrawImage(img, op)
		} else {
			ebitenutil.DrawRect(screen, float64(x)-2, float64(y)-2, 4, 4, color.RGBA{0, 0, 0xff, 0xff})
		}
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%d", p.PictID), x+4, y-8)
	}

	for _, m := range mobiles {
		x := int(m.H) + 320
		y := int(m.V) + 240
		var img *ebiten.Image
		if d, ok := descMap[m.Index]; ok {
			img = loadImage(d.PictID)
		}
		if img != nil {
			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(x), float64(y))
			screen.DrawImage(img, op)
		} else {
			ebitenutil.DrawRect(screen, float64(x)-3, float64(y)-3, 6, 6, color.RGBA{0xff, 0, 0, 0xff})
		}
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%d", m.Index), x+6, y-8)
	}

	lines := make([]string, 0, len(descs))
	for _, d := range descs {
		lines = append(lines, fmt.Sprintf("%d:%s id=%d t=%d", d.Index, d.Name, d.PictID, d.Type))
	}
	ebitenutil.DebugPrintAt(screen, strings.Join(lines, "\n"), 4, 4)
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("desc:%d pict:%d mobile:%d", len(descs), len(pics), len(mobiles)), 4, 460)

	msgs := getMessages()
	startY := 480 - 12*len(msgs)
	for i, msg := range msgs {
		ebitenutil.DebugPrintAt(screen, msg, 4, startY+12*i)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return 640, 480
}

func runGame(ctx context.Context) {
	gameCtx = ctx
	ebiten.SetWindowSize(640, 480)
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

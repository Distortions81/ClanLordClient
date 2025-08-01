package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"image/color"
	"log"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

var mouseX, mouseY = uint16(rand.Intn(1600)), uint16(rand.Intn(1200))

var gameCtx context.Context

// drawState tracks information needed by the Ebiten renderer.
type drawState struct {
	descriptors []frameDescriptor
	pictures    []framePicture
	mobiles     []frameMobile
}

var (
	state   drawState
	stateMu sync.Mutex
)

type Game struct{}

func (g *Game) Update() error {
	select {
	case <-gameCtx.Done():
		return fmt.Errorf("context done")
	default:
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.White)
	stateMu.Lock()
	mobiles := append([]frameMobile(nil), state.mobiles...)
	stateMu.Unlock()
	for _, m := range mobiles {
		x := int(m.H) + 320
		y := int(m.V) + 240
		ebitenutil.DrawRect(screen, float64(x), float64(y), 10, 10, color.RGBA{0xff, 0, 0, 0xff})
	}
	ebitenutil.DebugPrint(screen, fmt.Sprintf("mobiles: %d", len(mobiles)))
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

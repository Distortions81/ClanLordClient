package main

import (
	"image/color"
	"math"
	"regexp"
	"strconv"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
)

type NightInfo struct {
	mu        sync.Mutex
	BaseLevel int
	Azimuth   int
	Cloudy    bool
	Flags     uint
	Level     int
}

var gNight NightInfo

var nightRE = regexp.MustCompile(`^/nt ([0-9]+) /sa ([-0-9]+) /cl ([01])`)

func (n *NightInfo) calcCurLevel() {
	delta := 0
	if n.Flags&kLightNoNightMods != 0 {
		n.Level = 0
	} else {
		if n.Flags&kLightAdjust25Pct != 0 {
			delta += 25
		}
		if n.Flags&kLightAdjust50Pct != 0 {
			delta += 50
		}
		if n.Flags&kLightAreaIsDarker != 0 {
			delta = -delta
		}
		n.Level = n.BaseLevel - delta
	}
	if n.Level < 0 {
		n.Level = 0
	} else if n.Level > 100 {
		n.Level = 100
	}
}

func (n *NightInfo) SetFlags(f uint) {
	n.mu.Lock()
	n.Flags = f
	n.calcCurLevel()
	n.mu.Unlock()
}

func parseNightCommand(s string) bool {
	m := nightRE.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	lvl, _ := strconv.Atoi(m[1])
	sa, _ := strconv.Atoi(m[2])
	cloudy := m[3] != "0"
	gNight.mu.Lock()
	gNight.BaseLevel = lvl
	gNight.Azimuth = sa
	gNight.Cloudy = cloudy
	gNight.calcCurLevel()
	gNight.mu.Unlock()
	return true
}

var (
	nightImg         *ebiten.Image
	nightImgLevel    int
	nightImgRedshift float64
)

func drawNightOverlay(screen *ebiten.Image) {
	gNight.mu.Lock()
	lvl := gNight.Level
	gNight.mu.Unlock()
	if lvl <= 0 {
		return
	}
	redshift := 1.0
	if nightImg == nil || nightImgLevel != lvl || nightImgRedshift != redshift {
		rebuildNightOverlay(lvl, redshift)
	}
	op := &ebiten.DrawImageOptions{CompositeMode: ebiten.CompositeModeMultiply}
	screen.DrawImage(nightImg, op)
}

func rebuildNightOverlay(level int, redshift float64) {
	w := gameAreaSizeX * scale
	h := gameAreaSizeY * scale
	img := ebiten.NewImage(w, h)
	lf := float64(level) / 100.0
	rim := 1.0 - lf
	center := rim
	if lf >= 0.5 {
		center = 0.5
	}
	cx := float64(w) / 2
	cy := float64(h) / 2
	radius := 325.0 * float64(scale)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := float64(x) - cx
			dy := float64(y) - cy
			t := math.Sqrt(dx*dx+dy*dy) / radius
			if t > 1 {
				t = 1
			}
			c := center*(1-t) + rim*t
			r := c * redshift
			if r > 1 {
				r = 1
			}
			clr := color.RGBA{
				R: uint8(r * 255),
				G: uint8(c * 255),
				B: uint8(c * 255),
				A: 255,
			}
			img.Set(x, y, clr)
		}
	}
	nightImg = img
	nightImgLevel = level
	nightImgRedshift = redshift
}

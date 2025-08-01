package main

import (
	"fmt"
	_ "image/png"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

// imageCache lazily loads images from the img directory. The files are
// expected to be named as <id>.png where <id> matches the picture ID sent by
// the server. Loading errors are logged via dlog and result in a nil image.
var (
	imageCache = make(map[uint16]*ebiten.Image)
	imageMu    sync.Mutex
)

// loadImage retrieves the image for the specified picture ID. Images are
// cached after the first load to avoid reopening files each frame.
func loadImage(id uint16) *ebiten.Image {
	imageMu.Lock()
	defer imageMu.Unlock()
	if img, ok := imageCache[id]; ok {
		return img
	}
	path := fmt.Sprintf("img/%d.png", id)
	img, _, err := ebitenutil.NewImageFromFile(path)
	if err != nil {
		dlog("load %s: %v", path, err)
		imageCache[id] = nil
		return nil
	}
	imageCache[id] = img
	return img
}

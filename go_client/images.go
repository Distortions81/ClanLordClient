package main

import (
	"fmt"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"

	"go_client/climg"
)

// imageCache lazily loads images from the img directory. The files are
// expected to be named as id-%04d.png where %04d is a zero padded picture ID
// sent by the server. Loading errors are logged via dlog and result in a nil
// image.
var (
	imageCache = make(map[uint16]*ebiten.Image)
	imageMu    sync.Mutex
	clImages   *climg.CLImages
)

// loadImage retrieves the image for the specified picture ID. Images are
// cached after the first load to avoid reopening files each frame.
func loadImage(id uint16) *ebiten.Image {
	imageMu.Lock()
	defer imageMu.Unlock()
	if img, ok := imageCache[id]; ok {
		return img
	}
	if clImages != nil {
		if img := clImages.Get(uint32(id)); img != nil {
			imageCache[id] = img
			return img
		}
	}
	path := fmt.Sprintf("img/id-%04d.png", id)
	img, _, err := ebitenutil.NewImageFromFile(path)
	if err != nil {
		dlog("load %s: %v", path, err)
		imageCache[id] = nil
		return nil
	}
	imageCache[id] = img
	return img
}

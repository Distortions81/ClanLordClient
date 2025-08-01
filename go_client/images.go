package main

import (
	"image"
	"log"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"

	"go_client/climg"
)

// imageCache lazily loads images from the CL_Images archive. If an image is not
// present, nil is cached to avoid repeated lookups.
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
			frames := clImages.NumFrames(uint32(id))
			if frames > 1 {
				frame := 9
				if frame >= frames {
					frame = 0
				}
				h := img.Bounds().Dy() / frames
				y0 := frame * h
				img = img.SubImage(image.Rect(0, y0, img.Bounds().Dx(), y0+h)).(*ebiten.Image)
			}
			imageCache[id] = img
			return img
		}
		log.Printf("missing image %d", id)
	} else {
		log.Printf("CL_Images not loaded when requesting image %d", id)
	}
	imageCache[id] = nil
	return nil
}

package main

import (
	"image"
	"image/color"
	"log"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"

	"go_client/climg"
)

// imageCache lazily loads images from the CL_Images archive. If an image is not
// present, nil is cached to avoid repeated lookups.
var (
	// imageCache holds a cropped version of the first frame of an image. It
	// is used for static pictures on the playfield.
	imageCache = make(map[uint16]*ebiten.Image)
	// sheetCache holds the full sprite sheet for a picture ID. These are
	// used when extracting individual mobile frames.
	sheetCache = make(map[uint16]*ebiten.Image)
	// mobileCache caches individual mobile frames keyed by picture ID and
	// state.
	mobileCache = make(map[uint32]*ebiten.Image)

	imageMu  sync.Mutex
	clImages *climg.CLImages
)

// addBorder returns a new image with a one pixel transparent border around img.
// This helps avoid texture bleeding when sprites are scaled or filtered.
func addBorder(img *ebiten.Image) *ebiten.Image {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	bordered := ebiten.NewImage(w+2, h+2)
	bordered.Fill(color.RGBA{0, 0, 0, 0})
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(1, 1)
	bordered.DrawImage(img, op)
	return bordered
}

// loadImage retrieves the image for the specified picture ID. Images are
// cached after the first load to avoid reopening files each frame.
// loadSheet retrieves the full sprite sheet for the specified picture ID.
func loadSheet(id uint16) *ebiten.Image {
	imageMu.Lock()
	if img, ok := sheetCache[id]; ok {
		imageMu.Unlock()
		return img
	}
	imageMu.Unlock()

	if clImages != nil {
		if img := clImages.Get(uint32(id)); img != nil {
			imageMu.Lock()
			sheetCache[id] = img
			imageMu.Unlock()
			return img
		}
		log.Printf("missing image %d", id)
	} else {
		log.Printf("CL_Images not loaded when requesting image %d", id)
	}

	imageMu.Lock()
	sheetCache[id] = nil
	imageMu.Unlock()
	return nil
}

// loadImage retrieves the first frame for the specified picture ID. Images are
// cached after the first load to avoid reopening files each frame.
func loadImage(id uint16) *ebiten.Image {
	imageMu.Lock()
	if img, ok := imageCache[id]; ok {
		imageMu.Unlock()
		return img
	}
	imageMu.Unlock()

	if sheet := loadSheet(id); sheet != nil {
		frames := clImages.NumFrames(uint32(id))
		if frames > 1 {
			h := sheet.Bounds().Dy() / frames
			sheet = sheet.SubImage(image.Rect(0, 0, sheet.Bounds().Dx(), h)).(*ebiten.Image)
		}
		sheet = addBorder(sheet)
		imageMu.Lock()
		imageCache[id] = sheet
		imageMu.Unlock()
		return sheet
	}

	imageMu.Lock()
	imageCache[id] = nil
	imageMu.Unlock()
	return nil
}

// loadMobileFrame retrieves a cropped frame from a mobile sprite sheet based on
// the state value provided by the server.
func loadMobileFrame(id uint16, state uint8) *ebiten.Image {
	key := uint32(id)<<8 | uint32(state)
	imageMu.Lock()
	if img, ok := mobileCache[key]; ok {
		imageMu.Unlock()
		return img
	}
	imageMu.Unlock()

	sheet := loadSheet(id)
	if sheet == nil {
		imageMu.Lock()
		mobileCache[key] = nil
		imageMu.Unlock()
		return nil
	}

	size := sheet.Bounds().Dx() / 16
	x := int(state&0x0F) * size
	y := int(state>>4) * size
	if x+size > sheet.Bounds().Dx() || y+size > sheet.Bounds().Dy() {
		imageMu.Lock()
		mobileCache[key] = nil
		imageMu.Unlock()
		return nil
	}
	frame := sheet.SubImage(image.Rect(x, y, x+size, y+size)).(*ebiten.Image)
	frame = addBorder(frame)
	imageMu.Lock()
	mobileCache[key] = frame
	imageMu.Unlock()
	return frame
}

package climg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"os"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
)

type dataLocation struct {
	offset       uint32
	size         uint32
	entryType    uint32
	id           uint32
	colorBytes   []uint16
	version      uint32
	imageID      uint32
	colorID      uint32
	checksum     uint32
	flags        uint32
	unusedFlags  uint32
	unusedFlags2 uint32
	lightingID   int32
	plane        int16
	numFrames    uint16

	numAnims       int16
	animFrameTable [16]int16
}

type CLImages struct {
	data   []byte
	idrefs map[uint32]*dataLocation
	colors map[uint32]*dataLocation
	images map[uint32]*dataLocation
	cache  map[uint32]*ebiten.Image
	mu     sync.Mutex
}

const (
	TYPE_IDREF = 0x50446635
	TYPE_IMAGE = 0x42697432
	TYPE_COLOR = 0x436c7273

	pictDefFlagTransparent = 0x8000
	pictDefBlendMask       = 0x0003
	pictDefCustomColors    = 0x2000
	maxCustomColors        = 30
)

func Load(path string) (*CLImages, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	var header uint16
	var entryCount uint32
	if err := binary.Read(r, binary.BigEndian, &header); err != nil {
		return nil, err
	}
	if header != 0xffff {
		return nil, fmt.Errorf("bad header")
	}
	if err := binary.Read(r, binary.BigEndian, &entryCount); err != nil {
		return nil, err
	}
	var pad1 uint32
	var pad2 uint16
	if err := binary.Read(r, binary.BigEndian, &pad1); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.BigEndian, &pad2); err != nil {
		return nil, err
	}

	imgs := &CLImages{
		data:   data,
		idrefs: make(map[uint32]*dataLocation, entryCount),
		colors: make(map[uint32]*dataLocation, entryCount),
		images: make(map[uint32]*dataLocation, entryCount),
		cache:  make(map[uint32]*ebiten.Image),
	}

	for i := uint32(0); i < entryCount; i++ {
		dl := &dataLocation{}
		if err := binary.Read(r, binary.BigEndian, &dl.offset); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &dl.size); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &dl.entryType); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &dl.id); err != nil {
			return nil, err
		}
		switch dl.entryType {
		case TYPE_IDREF:
			imgs.idrefs[dl.id] = dl
		case TYPE_COLOR:
			imgs.colors[dl.id] = dl
		case TYPE_IMAGE:
			imgs.images[dl.id] = dl
		}
	}

	// populate IDREF data
	for _, ref := range imgs.idrefs {
		if _, err := r.Seek(int64(ref.offset), io.SeekStart); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.version); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.imageID); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.colorID); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.checksum); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.flags); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.unusedFlags); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.unusedFlags2); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.lightingID); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.plane); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.numFrames); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.BigEndian, &ref.numAnims); err != nil {
			return nil, err
		}
		for i := int16(0); i < ref.numAnims && i < 16; i++ {
			if err := binary.Read(r, binary.BigEndian, &ref.animFrameTable[i]); err != nil {
				return nil, err
			}
		}
		for i := ref.numAnims; i < 16; i++ {
			var tmp int16
			if err := binary.Read(r, binary.BigEndian, &tmp); err != nil {
				return nil, err
			}
		}
	}

	// preload colors
	for _, c := range imgs.colors {
		if _, err := r.Seek(int64(c.offset), io.SeekStart); err != nil {
			return nil, err
		}
		c.colorBytes = make([]uint16, c.size)
		for i := 0; i < int(c.size); i++ {
			b, err := r.ReadByte()
			if err != nil {
				return nil, err
			}
			c.colorBytes[i] = uint16(b)
		}
	}
	return imgs, nil
}

func (c *CLImages) Get(id uint32) *ebiten.Image {
	c.mu.Lock()
	if img, ok := c.cache[id]; ok {
		c.mu.Unlock()
		return img
	}
	c.mu.Unlock()

	ref := c.idrefs[id]
	if ref == nil {
		return nil
	}
	imgLoc := c.images[ref.imageID]
	colLoc := c.colors[ref.colorID]
	if imgLoc == nil || colLoc == nil {
		return nil
	}

	r := bytes.NewReader(c.data)
	if _, err := r.Seek(int64(imgLoc.offset), io.SeekStart); err != nil {
		log.Printf("seek image %d: %v", id, err)
		return nil
	}

	var h, w uint16
	var pad uint32
	var v, b byte
	if err := binary.Read(r, binary.BigEndian, &h); err != nil {
		log.Printf("read h for %d: %v", id, err)
		return nil
	}
	if err := binary.Read(r, binary.BigEndian, &w); err != nil {
		log.Printf("read w for %d: %v", id, err)
		return nil
	}
	if err := binary.Read(r, binary.BigEndian, &pad); err != nil {
		log.Printf("read pad for %d: %v", id, err)
		return nil
	}
	if err := binary.Read(r, binary.BigEndian, &v); err != nil {
		log.Printf("read v for %d: %v", id, err)
		return nil
	}
	if err := binary.Read(r, binary.BigEndian, &b); err != nil {
		log.Printf("read b for %d: %v", id, err)
		return nil
	}

	width := int(w)
	height := int(h)
	valueW := int(v)
	blockLenW := int(b)
	pixelCount := width * height
	br := New(r)
	data := make([]byte, pixelCount)
	pixPos := 0
	for pixPos < pixelCount {
		t, err := br.ReadBit()
		if err != nil {
			log.Printf("read bit for %d: %v", id, err)
			return nil
		}
		s, err := br.ReadInt(blockLenW)
		if err != nil {
			log.Printf("read int for %d: %v", id, err)
			return nil
		}
		s++
		if t {
			for i := 0; i < s; i++ {
				val, err := br.ReadBits(valueW)
				if err != nil {
					log.Printf("read bits for %d: %v", id, err)
					return nil
				}
				if pixPos < pixelCount {
					data[pixPos] = val
					pixPos++
				} else {
					break
				}
			}
		} else {
			val, err := br.ReadBits(valueW)
			if err != nil {
				log.Printf("read bits for %d: %v", id, err)
				return nil
			}
			for i := 0; i < s; i++ {
				if pixPos < pixelCount {
					data[pixPos] = val
					pixPos++
				} else {
					break
				}
			}
		}
	}

	// prepare color table and handle custom palette row if present
	pal := palette // from palette.go
	col := append([]uint16(nil), colLoc.colorBytes...)

	// If the image embeds a custom color lookup row, use its first few
	// pixels to remap the initial entries of the color table. Skip the
	// transparent color at index 0 and limit the remap to the known
	// custom-color count so we don't clobber unrelated palette entries.
	// Drop the palette row from the pixel data before constructing the
	// final image so the strip doesn't render onscreen.
	if ref.flags&pictDefCustomColors != 0 {
		orig := append([]uint16(nil), col...)
		n := maxCustomColors
		if n > width {
			n = width
		}
		if n > len(data) {
			n = len(data)
		}
		for i := 0; i < n && i+1 < len(col); i++ {
			idx := int(data[i])
			if idx < len(orig) {
				col[i+1] = orig[idx]
			}
		}
		data = data[width:]
		height--
	}
	pixelCount = len(data)
	img := image.NewRGBA(image.Rect(0, 0, width, height))

	alpha := uint8(255)
	switch ref.flags & pictDefBlendMask {
	case 1:
		alpha = 0xBF
	case 2:
		alpha = 0x7F
	case 3:
		alpha = 0x3F
	}
	transparent := (ref.flags & pictDefFlagTransparent) != 0

	for i := 0; i < pixelCount; i++ {
		idx := col[data[i]]
		r := uint8(pal[idx*3])
		g := uint8(pal[idx*3+1])
		b := uint8(pal[idx*3+2])
		a := alpha
		if idx == 0 && transparent {
			a = 0
		}
		// Ebiten expects premultiplied alpha values.
		r = uint8(int(r) * int(a) / 255)
		g = uint8(int(g) * int(a) / 255)
		b = uint8(int(b) * int(a) / 255)
		img.SetRGBA(i%width, i/width, color.RGBA{r, g, b, a})
	}

	eimg := ebiten.NewImageFromImage(img)
	c.mu.Lock()
	c.cache[id] = eimg
	c.mu.Unlock()
	return eimg
}

// NumFrames returns the number of animation frames for the given image ID.
// If unknown, it returns 1.
func (c *CLImages) NumFrames(id uint32) int {
	if ref := c.idrefs[id]; ref != nil && ref.numFrames > 0 {
		return int(ref.numFrames)
	}
	return 1
}

// Plane returns the drawing plane for the given image ID. If unknown, it
// returns 0.
func (c *CLImages) Plane(id uint32) int {
	if ref := c.idrefs[id]; ref != nil {
		return int(ref.plane)
	}
	return 0
}

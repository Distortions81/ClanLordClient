package climg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"
)

type dataLocation struct {
	offset     uint32
	size       uint32
	entryType  uint32
	id         uint32
	colorBytes []uint16
	imageID    uint32
	colorID    uint32
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
)

func Load(path string) (*CLImages, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r := bytes.NewReader(data)
	var header uint16
	var entryCount uint32
	binary.Read(r, binary.BigEndian, &header)
	if header != 0xffff {
		return nil, fmt.Errorf("bad header")
	}
	binary.Read(r, binary.BigEndian, &entryCount)
	var pad1 uint32
	var pad2 uint16
	binary.Read(r, binary.BigEndian, &pad1)
	binary.Read(r, binary.BigEndian, &pad2)

	imgs := &CLImages{
		data:   data,
		idrefs: make(map[uint32]*dataLocation, entryCount),
		colors: make(map[uint32]*dataLocation, entryCount),
		images: make(map[uint32]*dataLocation, entryCount),
		cache:  make(map[uint32]*ebiten.Image),
	}

	for i := uint32(0); i < entryCount; i++ {
		dl := &dataLocation{}
		binary.Read(r, binary.BigEndian, &dl.offset)
		binary.Read(r, binary.BigEndian, &dl.size)
		binary.Read(r, binary.BigEndian, &dl.entryType)
		binary.Read(r, binary.BigEndian, &dl.id)
		switch dl.entryType {
		case TYPE_IDREF:
			imgs.idrefs[dl.id] = dl
		case TYPE_COLOR:
			imgs.colors[dl.id] = dl
		case TYPE_IMAGE:
			imgs.images[dl.id] = dl
		}
	}

	// preload colors
	for _, c := range imgs.colors {
		r.Seek(int64(c.offset), io.SeekStart)
		c.colorBytes = make([]uint16, c.size)
		for i := 0; i < int(c.size); i++ {
			b, _ := r.ReadByte()
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
	r.Seek(int64(imgLoc.offset), io.SeekStart)

	var h, w uint16
	var pad uint32
	var v, b byte
	binary.Read(r, binary.BigEndian, &h)
	binary.Read(r, binary.BigEndian, &w)
	binary.Read(r, binary.BigEndian, &pad)
	binary.Read(r, binary.BigEndian, &v)
	binary.Read(r, binary.BigEndian, &b)

	width := int(w)
	height := int(h)
	valueW := int(v)
	blockLenW := int(b)
	pixelCount := width * height
	br := New(r)
	data := make([]byte, pixelCount)
	pixPos := 0
	for pixPos < pixelCount {
		t, _ := br.ReadBit()
		s, _ := br.ReadInt(blockLenW)
		s++
		if t {
			for i := 0; i < s; i++ {
				val, _ := br.ReadBits(valueW)
				if pixPos < pixelCount {
					data[pixPos] = val
					pixPos++
				} else {
					break
				}
			}
		} else {
			val, _ := br.ReadBits(valueW)
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

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	pal := palette // from palette.go
	col := colLoc.colorBytes
	for i := 0; i < pixelCount; i++ {
		idx := col[data[i]]
		r := uint8(pal[idx*3])
		g := uint8(pal[idx*3+1])
		b := uint8(pal[idx*3+2])
		a := uint8(255)
		if idx == 0 {
			a = 0
		}
		img.SetRGBA(i%width, i/width, color.RGBA{r, g, b, a})
	}

	eimg := ebiten.NewImageFromImage(img)
	c.mu.Lock()
	c.cache[id] = eimg
	c.mu.Unlock()
	return eimg
}

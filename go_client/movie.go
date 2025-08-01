package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	flagStale        = 0x01
	flagMobileData   = 0x02
	flagGameState    = 0x04
	flagPictureTable = 0x08
)

const movieSignature = 0xdeadbeef
const oldestMovieVersion = 193

func parseMovie(path string) ([][]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < 8 {
		return nil, fmt.Errorf("short file")
	}
	if binary.BigEndian.Uint32(data[:4]) != movieSignature {
		return nil, fmt.Errorf("bad signature")
	}
	version := binary.BigEndian.Uint16(data[4:6])
	// Arindal movies store version numbers 100x larger.
	if version > 50000 {
		version /= 100
	}
	if version < oldestMovieVersion {
		return nil, fmt.Errorf("movie version too old: %d", version)
	}
	headerLen := int(binary.BigEndian.Uint16(data[6:8]))
	if headerLen <= 0 || headerLen > len(data) {
		headerLen = 24
	}
	dlog("movie version %d headerLen %d", version, headerLen)
	pos := headerLen
	sign := []byte{0xde, 0xad, 0xbe, 0xef}
	frames := [][]byte{}
	frameNum := 0
	for pos+12 <= len(data) {
		if binary.BigEndian.Uint32(data[pos:pos+4]) != movieSignature {
			idx := bytes.Index(data[pos:], sign)
			if idx < 0 {
				break
			}
			pos += idx
			continue
		}
		frame := binary.BigEndian.Uint32(data[pos+4 : pos+8])
		size := int(binary.BigEndian.Uint16(data[pos+8 : pos+10]))
		flags := binary.BigEndian.Uint16(data[pos+10 : pos+12])
		dlog("frame %d index=%d size=%d flags=0x%x", frameNum, frame, size, flags)
		pos += 12
		if flags&flagGameState != 0 {
			dlog("GameState block at %d", pos)
			if pos+24 > len(data) {
				break
			}
			maxSize := int(binary.BigEndian.Uint32(data[pos+12 : pos+16]))
			pos += 24 + maxSize
			continue
		}
		if flags&flagMobileData != 0 {
			dlog("MobileData table at %d", pos)
			// Descriptor layouts vary between client versions and
			// are difficult to decode generically. For now simply
			// skip until the sentinel value -1 which terminates the
			// table. This keeps subsequent frame parsing in sync.
			if idx := bytes.Index(data[pos:], []byte{0xff, 0xff, 0xff, 0xff}); idx >= 0 {
				pos += idx + 4
			} else {
				pos = len(data)
			}
			continue
		}
		if flags&flagPictureTable != 0 {
			dlog("PictureTable at %d", pos)
			if pos+2 > len(data) {
				break
			}
			count := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2
			pics := make([]framePicture, 0, count)
			for i := 0; i < count && pos+6 <= len(data); i++ {
				id := binary.BigEndian.Uint16(data[pos : pos+2])
				h := int16(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
				v := int16(binary.BigEndian.Uint16(data[pos+4 : pos+6]))
				pos += 6
				pics = append(pics, framePicture{PictID: id, H: h, V: v})
			}
			if pos+4 <= len(data) {
				pos += 4
			}
			stateMu.Lock()
			state.pictures = pics
			stateMu.Unlock()
			continue
		}
		if size > 0 {
			if pos+size > len(data) {
				break
			}
			frames = append(frames, append([]byte(nil), data[pos:pos+size]...))
			pos += size
		} else {
			idx := bytes.Index(data[pos:], sign)
			if idx < 0 {
				break
			}
			pos += idx
		}
		frameNum++
	}
	return frames, nil
}

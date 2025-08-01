package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
)

const movieSignature = 0xdeadbeef

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
	headerLen := int(binary.BigEndian.Uint16(data[6:8]))
	if headerLen <= 0 || headerLen > len(data) {
		headerLen = 24
	}
	pos := headerLen
	sign := []byte{0xde, 0xad, 0xbe, 0xef}
	frames := [][]byte{}
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
		_ = frame
		_ = flags
		pos += 12
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
	}
	return frames, nil
}

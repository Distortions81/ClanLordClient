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

var movieRevision int32

func parseMovie(path string, clientVersion int) ([][]byte, error) {
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
	if len(data) < headerLen {
		return nil, fmt.Errorf("short file")
	}
	frameCount := int32(binary.BigEndian.Uint32(data[8:12]))
	startTime := binary.BigEndian.Uint32(data[12:16])
	var revision, oldestReader int32
	if headerLen >= 24 {
		if len(data) < 24 {
			return nil, fmt.Errorf("short file")
		}
		revision = int32(binary.BigEndian.Uint32(data[16:20]))
		oldestReader = int32(binary.BigEndian.Uint32(data[20:24]))
	}
	movieRevision = revision
	if oldestReader != 0 && oldestReader > int32(clientVersion) {
		return nil, fmt.Errorf("movie requires newer client: %d", oldestReader)
	}
	dlog("movie version %d headerLen %d frames %d starttime %d revision %d oldestReader %d", version, headerLen, frameCount, startTime, revision, oldestReader)
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
			pos = parseMobileTable(data, pos, revision)
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

func parseMobileTable(data []byte, pos int, revision int32) int {
	_ = revision
	const (
		descTableSize = 266
		descSize      = 156
		colorsOffset  = 52
		nameOffset    = 82
	)
	for pos+4 <= len(data) {
		idx := int32(binary.BigEndian.Uint32(data[pos : pos+4]))
		pos += 4
		if idx == -1 {
			break
		}
		hasMobile := idx < descTableSize
		if !hasMobile {
			idx -= descTableSize
		}
		var mob frameMobile
		if hasMobile {
			if pos+16 > len(data) {
				return len(data)
			}
			mob.Index = uint8(idx)
			mob.State = uint8(binary.BigEndian.Uint32(data[pos : pos+4]))
			mob.H = int16(binary.BigEndian.Uint32(data[pos+4 : pos+8]))
			mob.V = int16(binary.BigEndian.Uint32(data[pos+8 : pos+12]))
			mob.Colors = uint8(binary.BigEndian.Uint32(data[pos+12 : pos+16]))
			pos += 16
		}
		if pos+descSize > len(data) {
			return len(data)
		}
		buf := data[pos : pos+descSize]
		pos += descSize
		d := frameDescriptor{Index: uint8(idx)}
		d.Type = uint8(binary.BigEndian.Uint32(buf[16:20]))
		pict := binary.BigEndian.Uint32(buf[0:4])
		d.PictID = uint16(pict & 0xffff)
		numColors := int(binary.BigEndian.Uint32(buf[44:48]))
		if numColors < 0 || numColors > 30 {
			numColors = 30
		}
		end := colorsOffset + numColors
		if end > len(buf) {
			end = len(buf)
		}
		d.Colors = append([]byte(nil), buf[colorsOffset:end]...)
		nameBytes := buf[nameOffset : nameOffset+48]
		if i := bytes.IndexByte(nameBytes, 0); i >= 0 {
			d.Name = string(nameBytes[:i])
		} else {
			d.Name = string(nameBytes)
		}
		bubbleCounter := int32(binary.BigEndian.Uint32(buf[28:32]))
		if bubbleCounter != 0 {
			if pos+2 > len(data) {
				return len(data)
			}
			l := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2 + l
		}
		stateMu.Lock()
		if hasMobile {
			if state.mobiles == nil {
				state.mobiles = make(map[uint8]frameMobile)
			}
			state.mobiles[mob.Index] = mob
		}
		if state.descriptors == nil {
			state.descriptors = make(map[uint8]frameDescriptor)
		}
		state.descriptors[d.Index] = d
		stateMu.Unlock()
	}
	return pos
}

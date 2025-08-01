package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// frameDescriptor describes an on-screen descriptor.
type frameDescriptor struct {
	Index  uint8
	Type   uint8
	PictID uint16
	Name   string
	Colors []byte
}

type framePicture struct {
	PictID uint16
	H, V   int16
}

type frameMobile struct {
	Index  uint8
	State  uint16
	V, H   int16
	Colors uint8
}

// bitReader helps decode the packed picture fields.
type bitReader struct {
	data   []byte
	bitPos int
}

func (br *bitReader) readBits(n int) uint32 {
	var v uint32
	for n > 0 {
		if br.bitPos/8 >= len(br.data) {
			return v
		}
		b := br.data[br.bitPos/8]
		remain := 8 - br.bitPos%8
		take := remain
		if take > n {
			take = n
		}
		shift := remain - take
		v = (v << take) | uint32((b>>shift)&((1<<take)-1))
		br.bitPos += take
		n -= take
	}
	return v
}

func signExtend(v uint32, bits int) int16 {
	if v&(1<<(bits-1)) != 0 {
		v |= ^uint32(0) << bits
	}
	return int16(int32(v))
}

// handleDrawState decodes the packed draw state message.
func handleDrawState(m []byte) {
	if len(m) < 11 { // 2 byte tag + 9 bytes minimum
		return
	}
	// Incoming draw state packets appear unencrypted.  Try decoding the
	// payload as-is first and fall back to the simple XOR scheme if that
	// fails.  The message begins with a 2 byte tag.
	data := append([]byte(nil), m[2:]...)

	if parseDrawState(data) {
		return
	}

	simpleEncrypt(data)
	if !parseDrawState(data) {
		dlog("failed to parse draw state: % x", data[:16])
	}
}

// parseDrawState decodes the draw state data. It returns false when the packet
// appears malformed.
func parseDrawState(data []byte) bool {
	if len(data) < 9 {
		return false
	}

	ackCmd := data[0]
	ackFrame = int32(binary.BigEndian.Uint32(data[1:5]))
	resendFrame = int32(binary.BigEndian.Uint32(data[5:9]))
	p := 9

	if len(data) <= p {
		return false
	}
	descCount := int(data[p])
	p++
	descs := make([]frameDescriptor, 0, descCount)
	for i := 0; i < descCount && p < len(data); i++ {
		if p+4 > len(data) {
			return false
		}
		d := frameDescriptor{}
		d.Index = data[p]
		d.Type = data[p+1]
		d.PictID = binary.BigEndian.Uint16(data[p+2:])
		p += 4
		if idx := bytes.IndexByte(data[p:], 0); idx >= 0 {
			d.Name = string(data[p : p+idx])
			p += idx + 1
		} else {
			return false
		}
		if p >= len(data) {
			return false
		}
		cnt := int(data[p])
		p++
		if p+cnt > len(data) {
			return false
		}
		d.Colors = append([]byte(nil), data[p:p+cnt]...)
		p += cnt
		descs = append(descs, d)
	}

	if len(data) < p+7 {
		return false
	}
	p += 7 // skip status fields

	if len(data) <= p {
		return false
	}
	pictCount := int(data[p])
	p++
	pictAgain := 0
	if pictCount == 255 {
		if len(data) < p+2 {
			return false
		}
		pictAgain = int(data[p])
		pictCount = int(data[p+1])
		p += 2
	}

	pics := make([]framePicture, 0, pictAgain+pictCount)
	br := bitReader{data: data[p:]}
	for i := 0; i < pictCount; i++ {
		id := uint16(br.readBits(14))
		h := signExtend(br.readBits(11), 11)
		v := signExtend(br.readBits(11), 11)
		pics = append(pics, framePicture{PictID: id, H: h, V: v})
	}
	p += br.bitPos / 8
	if br.bitPos%8 != 0 {
		p++
	}

	if len(data) <= p {
		return false
	}
	mobileCount := int(data[p])
	p++
	mobiles := make([]frameMobile, 0, mobileCount)
	for i := 0; i < mobileCount && p+8 <= len(data); i++ {
		m := frameMobile{}
		m.Index = data[p]
		m.State = binary.BigEndian.Uint16(data[p+1:])
		m.V = int16(binary.BigEndian.Uint16(data[p+3:]))
		m.H = int16(binary.BigEndian.Uint16(data[p+5:]))
		m.Colors = data[p+7]
		p += 8
		mobiles = append(mobiles, m)
	}

	stateData := data[p:]

	stateMu.Lock()
	if state.descriptors == nil {
		state.descriptors = make(map[uint8]frameDescriptor)
	}
	for _, d := range descs {
		state.descriptors[d.Index] = d
	}

	// retain previously drawn pictures when the packet specifies pictAgain
	again := pictAgain
	if again > len(state.pictures) {
		again = len(state.pictures)
	}
	newPics := make([]framePicture, again+pictCount)
	copy(newPics, state.pictures[:again])
	copy(newPics[again:], pics)
	state.pictures = newPics

	if state.mobiles == nil {
		state.mobiles = make(map[uint8]frameMobile)
	}
	for _, m := range mobiles {
		state.mobiles[m.Index] = m
	}
	stateMu.Unlock()

	dlog("draw state cmd=%d ack=%d resend=%d desc=%d pict=%d again=%d mobile=%d state=%d",
		ackCmd, ackFrame, resendFrame, len(descs), len(pics), pictAgain, len(mobiles), len(stateData))

	if txt := decodeBEPP(stateData); txt != "" {
		fmt.Println(txt)
	} else if txt := decodeBubble(stateData); txt != "" {
		fmt.Println(txt)
	}
	return true
}

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"time"
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
	State  uint8
	H, V   int16
	Colors uint8
}

const poseDead = 32

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

// picturesSummary returns a compact string of picture IDs and coordinates for
// debugging. At most the first 8 entries are included.
func picturesSummary(pics []framePicture) string {
	const max = 8
	var buf bytes.Buffer
	for i, p := range pics {
		if i >= max {
			buf.WriteString("...")
			break
		}
		fmt.Fprintf(&buf, "%d:(%d,%d) ", p.PictID, p.H, p.V)
	}
	return buf.String()
}

// onScreen reports whether the picture lies within the visible playfield.
func onScreen(p framePicture) bool {
	x := int(p.H) + fieldCenterX
	y := int(p.V) + fieldCenterY
	return x >= 0 && x < gameAreaSizeX && y >= 0 && y < gameAreaSizeY
}

// pictureShift returns the (dx, dy) movement that most on-screen pictures agree on
// between two consecutive frames. Pictures are matched by PictID (duplicates
// included). The boolean result is false when no majority offset is found.
func pictureShift(prev, cur []framePicture) (int, int, bool) {
	if len(prev) == 0 || len(cur) == 0 {
		dlog("pictureShift: no data prev=%d cur=%d", len(prev), len(cur))
		return 0, 0, false
	}

	counts := make(map[[2]int]int)
	total := 0
	maxInt := int(^uint(0) >> 1)
	for _, p := range prev {
		if !onScreen(p) {
			continue
		}
		bestDist := maxInt
		var bestDx, bestDy int
		matched := false
		for _, c := range cur {
			if p.PictID != c.PictID || !onScreen(c) {
				continue
			}
			dx := int(c.H) - int(p.H)
			dy := int(c.V) - int(p.V)
			dist := dx*dx + dy*dy
			if dist < bestDist {
				bestDist = dist
				bestDx = dx
				bestDy = dy
				matched = true
			}
		}
		if matched {
			counts[[2]int{bestDx, bestDy}]++
			total++
		}
	}
	if total == 0 {
		dlog("pictureShift: no matching pairs")
		return 0, 0, false
	}

	best := [2]int{}
	bestCount := 0
	for k, c := range counts {
		if c > bestCount {
			best = k
			bestCount = c
		}
	}
	dlog("pictureShift: counts=%v best=%v count=%d total=%d", counts, best, bestCount, total)
	if bestCount*2 <= total {
		dlog("pictureShift: no majority best=%d total=%d", bestCount, total)
		return 0, 0, false
	}
	return best[0], best[1], true
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
		if p+6 > len(data) {
			return false
		}
		d := frameDescriptor{}
		d.Index = data[p]
		d.Type = data[p+1]
		d.PictID = binary.BigEndian.Uint16(data[p+2:])
		nameLen := int(data[p+4])
		colorLen := int(data[p+5])
		p += 6
		if p+nameLen > len(data) {
			return false
		}
		d.Name = string(data[p : p+nameLen])
		p += nameLen
		if d.Name == playerName {
			playerIndex = d.Index
		}
		if p+colorLen > len(data) {
			return false
		}
		d.Colors = append([]byte(nil), data[p:p+colorLen]...)
		p += colorLen
		updatePlayerAppearance(d.Name, d.PictID, d.Colors)
		descs = append(descs, d)
	}

	if len(data) < p+7 {
		return false
	}
	hp := int(data[p])
	hpMax := int(data[p+1])
	sp := int(data[p+2])
	spMax := int(data[p+3])
	bal := int(data[p+4])
	balMax := int(data[p+5])
	// lighting := data[p+6]
	p += 7

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
	for i := 0; i < mobileCount && p+7 <= len(data); i++ {
		m := frameMobile{}
		m.Index = data[p]
		m.State = data[p+1]
		m.H = int16(binary.BigEndian.Uint16(data[p+2:]))
		m.V = int16(binary.BigEndian.Uint16(data[p+4:]))
		m.Colors = data[p+6]
		p += 7
		mobiles = append(mobiles, m)
	}

	stateData := data[p:]

	stateMu.Lock()
	state.hp = hp
	state.hpMax = hpMax
	state.sp = sp
	state.spMax = spMax
	state.balance = bal
	state.balanceMax = balMax
	changed := false
	if onion {
		if len(descs) > 0 {
			changed = true
		}
		if len(mobiles) != len(state.mobiles) {
			changed = true
		} else {
			for _, m := range mobiles {
				if pm, ok := state.mobiles[m.Index]; !ok || pm.State != m.State {
					changed = true
					break
				}
			}
		}
		if changed {
			if state.prevDescs == nil {
				state.prevDescs = make(map[uint8]frameDescriptor)
			}
			state.prevDescs = make(map[uint8]frameDescriptor, len(state.descriptors))
			for idx, d := range state.descriptors {
				state.prevDescs[idx] = d
			}
		}
	}
	if state.descriptors == nil {
		state.descriptors = make(map[uint8]frameDescriptor)
	}
	for _, d := range descs {
		state.descriptors[d.Index] = d
	}

	// retain previously drawn pictures when the packet specifies pictAgain
	prevPics := state.pictures
	again := pictAgain
	if again > len(prevPics) {
		again = len(prevPics)
	}
	newPics := make([]framePicture, again+pictCount)
	copy(newPics, prevPics[:again])
	copy(newPics[again:], pics)
	if interp {
		dx, dy, ok := pictureShift(prevPics, newPics)
		dlog("interp pictures again=%d prev=%d cur=%d shift=(%d,%d) ok=%t", again, len(prevPics), len(newPics), dx, dy, ok)
		if !ok {
			dlog("prev pics: %s", picturesSummary(prevPics))
			dlog("new  pics: %s", picturesSummary(newPics))
		}
		if ok {
			state.picShiftX = dx
			state.picShiftY = dy
		} else {
			state.picShiftX = 0
			state.picShiftY = 0
		}
	}
	state.pictures = newPics

	needAnimUpdate := interp || (onion && changed)
	if needAnimUpdate {
		// save previous mobile positions for interpolation and fading
		if state.prevMobiles == nil {
			state.prevMobiles = make(map[uint8]frameMobile)
		}
		// copy current mobiles to prevMobiles before replacing
		state.prevMobiles = make(map[uint8]frameMobile, len(state.mobiles))
		for idx, m := range state.mobiles {
			state.prevMobiles[idx] = m
		}
		const defaultInterval = time.Second / 5
		interval := defaultInterval
		if !state.prevTime.IsZero() && !state.curTime.IsZero() {
			if d := state.curTime.Sub(state.prevTime); d > 0 {
				interval = d
			}
		}
		dlog("interp mobiles interval=%s", interval)
		state.prevTime = time.Now()
		state.curTime = state.prevTime.Add(interval)
	}

	if state.mobiles == nil {
		state.mobiles = make(map[uint8]frameMobile)
	} else {
		// clear map while keeping allocation
		for k := range state.mobiles {
			delete(state.mobiles, k)
		}
	}
	for _, m := range mobiles {
		state.mobiles[m.Index] = m
	}
	stateMu.Unlock()

	dlog("draw state cmd=%d ack=%d resend=%d desc=%d pict=%d again=%d mobile=%d state=%d",
		ackCmd, ackFrame, resendFrame, len(descs), len(pics), pictAgain, len(mobiles), len(stateData))

	if idx := bytes.IndexByte(stateData, 0); idx >= 0 {
		handleInfoText(stateData[:idx])
		stateData = stateData[idx+1:]
	} else {
		return true
	}

	if len(stateData) > 0 {
		bubbleCount := int(stateData[0])
		stateData = stateData[1:]
		for i := 0; i < bubbleCount; i++ {
			if len(stateData) < 2 {
				return false
			}
			idx := stateData[0]
			typ := int(stateData[1])
			p := 2
			if typ&kBubbleNotCommon != 0 {
				if len(stateData) < p+1 {
					return false
				}
				p++
			}
			if typ&kBubbleFar != 0 {
				if len(stateData) < p+4 {
					return false
				}
				p += 4
			}
			if len(stateData) < p {
				return false
			}
			end := bytes.IndexByte(stateData[p:], 0)
			if end < 0 {
				return false
			}
			bubbleData := stateData[:p+end+1]
			if txt := decodeBubble(bubbleData); txt != "" {
				name := ""
				stateMu.Lock()
				if d, ok := state.descriptors[idx]; ok {
					name = d.Name
				}
				stateMu.Unlock()
				msg := txt
				if name != "" {
					msg = name + " " + txt
				}
				fmt.Println(msg)
				if idx != playerIndex {
					addMessage(msg)
				}
			}
			stateData = stateData[p+end+1:]
		}
	}
	return true
}

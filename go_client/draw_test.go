package main

import (
	"encoding/binary"
	"testing"
)

// TestParseDrawStatePreservesName verifies that descriptors with empty names do not
// overwrite previously known names during movie playback.
func TestParseDrawStatePreservesName(t *testing.T) {
	debug = false
	players = make(map[string]*Player)
	state.descriptors = map[uint8]frameDescriptor{
		1: {Index: 1, Name: "Alice"},
	}
	state.mobiles = make(map[uint8]frameMobile)

	data := []byte{
		0,          // ackCmd
		0, 0, 0, 0, // ackFrame
		0, 0, 0, 0, // resendFrame
		1,    // descCount
		1,    // index
		0,    // type
		0, 0, // pictID
		0,    // name (empty) terminator
		1, 0, // color count + data
		0, 0, 0, 0, 0, 0, 0, // stats (hp..lighting)
		0, // pictCount
		0, // mobileCount
		0, // info text terminator
		0, // bubble count
	}
	msg := make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(msg[:2], 2)
	copy(msg[2:], data)

	handleDrawState(msg)

	if d, ok := state.descriptors[1]; !ok || d.Name != "Alice" {
		t.Fatalf("descriptor name not preserved: %+v", d)
	}
	if _, ok := players[""]; ok {
		t.Fatalf("empty player name registered")
	}
}

// TestParseDrawStatePreservesMobiles ensures that incoming draw state packets
// with zero mobile entries do not clear previously known mobiles. Without this
// behavior player names disappear during movie playback.
func TestParseDrawStatePreservesMobiles(t *testing.T) {
	debug = false
	players = make(map[string]*Player)
	state.descriptors = map[uint8]frameDescriptor{}
	state.mobiles = map[uint8]frameMobile{
		1: {Index: 1, H: 10, V: 20},
	}

	data := []byte{
		0,          // ackCmd
		0, 0, 0, 0, // ackFrame
		0, 0, 0, 0, // resendFrame
		0,                   // descCount
		0, 0, 0, 0, 0, 0, 0, // stats (hp..lighting)
		0, // pictCount
		0, // mobileCount
		0, // info text terminator
		0, // bubble count
	}
	msg := make([]byte, 2+len(data))
	binary.BigEndian.PutUint16(msg[:2], 2)
	copy(msg[2:], data)

	handleDrawState(msg)

	if len(state.mobiles) != 1 {
		t.Fatalf("mobiles cleared: %#v", state.mobiles)
	}
}

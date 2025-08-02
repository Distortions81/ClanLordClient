package main

import "testing"

func TestParseDrawStateDescriptorName(t *testing.T) {
	// reset global state
	state.descriptors = make(map[uint8]frameDescriptor)
	players = make(map[string]*Player)

	data := []byte{
		0,          // ackCmd
		0, 0, 0, 0, // ackFrame
		0, 0, 0, 0, // resendFrame
		1,          // descriptor count
		1,          // index
		2,          // type
		0x12, 0x34, // pictID
		3,             // name length
		2,             // color count
		'B', 'o', 'b', // name
		1, 2, // colors
		10, 20, 5, 10, 7, 8, 0, // hp/hpMax/sp/spMax/balance/balanceMax/lighting
		0, // picture count
		0, // mobile count
	}

	if !parseDrawState(data) {
		t.Fatalf("parseDrawState returned false")
	}
	d, ok := state.descriptors[1]
	if !ok {
		t.Fatalf("descriptor not found")
	}
	if d.Name != "Bob" {
		t.Fatalf("got name %q", d.Name)
	}
	p := players["Bob"]
	if p == nil || p.PictID != 0x1234 {
		t.Fatalf("player not updated: %#v", p)
	}
}

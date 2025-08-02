package main

import "testing"

func TestParseMovie(t *testing.T) {
	debug = false
	players = make(map[string]*Player)
	state.descriptors = make(map[uint8]frameDescriptor)
	state.mobiles = make(map[uint8]frameMobile)
	frames, err := parseMovie("test.clMov")
	if err != nil {
		t.Fatalf("parseMovie: %v", err)
	}
	if len(frames) == 0 {
		t.Fatalf("no frames parsed")
	}
}

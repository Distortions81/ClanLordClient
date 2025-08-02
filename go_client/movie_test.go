package main

import "testing"

func TestParseMovieRegistersPlayers(t *testing.T) {
	players = make(map[string]*Player)
	state.descriptors = make(map[uint8]frameDescriptor)
	state.mobiles = make(map[uint8]frameMobile)
	if _, err := parseMovie("test.clMov"); err != nil {
		t.Fatalf("parseMovie: %v", err)
	}
	if len(players) == 0 {
		t.Fatalf("players not registered")
	}
}

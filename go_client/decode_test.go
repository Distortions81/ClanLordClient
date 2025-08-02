package main

import (
	"encoding/binary"
	"testing"
)

func TestDecodeBubbleStripsTags(t *testing.T) {
	data := []byte{0x00, byte(kBubbleWhisper), 0x8A, 0xC2, 'p', 'n', ' ', 'p', 'i', 'n', 'g', '!', 0}
	got := decodeBubble(data)
	if got != "whisper: ping!" {
		t.Fatalf("got %q", got)
	}
}

func TestDecodeBubbleEmptyAfterStripping(t *testing.T) {
	data := []byte{0x00, byte(kBubbleNormal), 0x8A, 0xC2, 'p', 'n', 0}
	if got := decodeBubble(data); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestParseBackendInfo(t *testing.T) {
	players = make(map[string]*Player)
	data := []byte("\xc2be\xc2in\xc2pnAlice\xc2pnHuman\tFemale\tFighter\t")
	decodeBEPP(data)
	p := players["Alice"]
	if p == nil || p.Class != "Fighter" || p.Race != "Human" {
		t.Fatalf("unexpected player: %#v", p)
	}
}

func TestParseBackendShare(t *testing.T) {
	players = make(map[string]*Player)
	data := []byte("\xc2be\xc2sh\xc2pnAlice\xc2pn,\xc2pnBob\xc2pn\t\xc2pnCarol\xc2pn")
	decodeBEPP(data)
	if !players["Alice"].Sharee || !players["Bob"].Sharee || !players["Carol"].Sharing {
		t.Fatalf("share parsing failed: %#v", players)
	}
}

func TestParseMovieNames(t *testing.T) {
	state.descriptors = nil
	state.mobiles = nil
	if _, err := parseMovie("test.clMov", 1440); err != nil {
		t.Fatalf("parseMovie: %v", err)
	}
	found := false
	for _, d := range state.descriptors {
		if d.Name != "" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no descriptor names parsed")
	}
}

func TestParseMobileTableVersions(t *testing.T) {
	cases := []struct {
		version uint16
		name    string
	}{
		{105, "v105"},
		{97, "v97"},
	}
	for _, tc := range cases {
		state.descriptors = nil
		state.mobiles = nil
		data := buildStubTable(tc.version, tc.name)
		parseMobileTable(data, 0, tc.version, 0)
		d := state.descriptors[0]
		if d.Name != tc.name {
			t.Fatalf("version %d got %q", tc.version, d.Name)
		}
	}
}

func buildStubTable(version uint16, name string) []byte {
	var descSize, nameOffset int
	switch {
	case version > 141:
		descSize, nameOffset = 156, 86
	case version > 113:
		descSize, nameOffset = 150, 82
	case version > 105:
		descSize, nameOffset = 142, 82
	case version > 97:
		descSize, nameOffset = 130, 70
	default:
		descSize, nameOffset = 126, 66
	}
	buf := make([]byte, 4+16+descSize+4)
	binary.BigEndian.PutUint32(buf[0:4], 0) // index
	// mobile (16 bytes) already zero
	copy(buf[4+16+nameOffset:], []byte(name))
	// numColors and bubble counter already zero
	binary.BigEndian.PutUint32(buf[4+16+descSize:], 0xffffffff) // terminator
	return buf
}

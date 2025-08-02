package main

import "testing"

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
	data := []byte("\xc2be\xc2in\xc2pnAlice\xc2pn\tHuman\tFemale\tFighter\t")
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

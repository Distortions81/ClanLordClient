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

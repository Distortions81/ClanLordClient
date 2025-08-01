package main

import "testing"

func TestParseMovie(t *testing.T) {
	frames, err := parseMovie("test.clMov")
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) == 0 {
		t.Fatalf("no frames parsed")
	}
	if len(frames[0]) == 0 {
		t.Fatalf("first frame empty")
	}
}

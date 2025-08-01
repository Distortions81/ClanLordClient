package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

type logMessage struct {
	Timestamp string `json:"timestamp"`
	Data      string `json:"data"`
}

func TestParseDrawStateFromLogs(t *testing.T) {
	f, err := os.Open("go_client/testdata/draw_state.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var msgs []logMessage
	if err := json.NewDecoder(f).Decode(&msgs); err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		buf, err := base64.StdEncoding.DecodeString(m.Data)
		if err != nil {
			t.Fatalf("decode base64 #%d: %v", i, err)
		}
		data := append([]byte(nil), buf[2:]...)
		if parseDrawState(data) {
			continue
		}
		simpleEncrypt(data)
		if !parseDrawState(data) {
			t.Fatalf("parseDrawState failed for message %d", i)
		}
	}
}

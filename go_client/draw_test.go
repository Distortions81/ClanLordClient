package main

import "testing"

func TestHandleDrawStateInfoStrings(t *testing.T) {
	messages = nil
	state = drawState{}
	// sample text snippets from test.clMov
	msg1 := "You sense healing energy from Harper."
	msg2 := "a fur, worth 37c. Your share is 3c."

	stateData := append([]byte(msg1), 0)
	stateData = append(stateData, []byte(msg2)...)
	stateData = append(stateData, 0) // terminator before bubble count
	stateData = append(stateData, 0) // bubble count 0

	data := make([]byte, 0, 19+len(stateData))
	data = append(data, 0)                  // ackCmd
	data = append(data, make([]byte, 8)...) // ackFrame + resendFrame
	data = append(data, 0)                  // descriptor count
	data = append(data, make([]byte, 7)...) // hp, hpMax, sp, spMax, bal, balMax, lighting
	data = append(data, 0)                  // picture count
	data = append(data, 0)                  // mobile count
	data = append(data, stateData...)

	m := append([]byte{0, 0}, data...)
	handleDrawState(m)

	got := getMessages()
	if len(got) != 2 || got[0] != msg1 || got[1] != msg2 {
		t.Fatalf("messages = %#v", got)
	}
}

func TestHandleDrawStateEncryptedInfoStrings(t *testing.T) {
	messages = nil
	state = drawState{}
	msg1 := "You sense healing energy from Harper."
	msg2 := "a fur, worth 37c. Your share is 3c."

	stateData := append([]byte(msg1), 0)
	stateData = append(stateData, []byte(msg2)...)
	stateData = append(stateData, 0)
	stateData = append(stateData, 0)

	data := make([]byte, 0, 19+len(stateData))
	data = append(data, 0)
	data = append(data, make([]byte, 8)...)
	data = append(data, 0)
	data = append(data, make([]byte, 7)...)
	data = append(data, 0)
	data = append(data, 0)
	data = append(data, stateData...)

	m := append([]byte{0, 0}, data...)
	simpleEncrypt(m[2:])
	handleDrawState(m)

	got := getMessages()
	if len(got) != 2 || got[0] != msg1 || got[1] != msg2 {
		t.Fatalf("messages = %#v", got)
	}
}

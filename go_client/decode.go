package main

import (
	"bytes"
	"strings"
)

func decodeBEPP(data []byte) string {
	if len(data) < 3 || data[0] != 0xC2 {
		return ""
	}
	prefix := string(data[1:3])
	text := strings.TrimRight(string(data[3:]), "\x00")
	switch prefix {
	case "th":
		return "think: " + text
	case "in":
		return "info: " + text
	case "sh":
		return "share: " + text
	default:
		return ""
	}
}

func decodeBubble(data []byte) string {
	if len(data) < 2 {
		return ""
	}
	typ := int(data[1])
	p := 2
	if typ&kBubbleNotCommon != 0 {
		if len(data) < p+1 {
			return ""
		}
		p++
	}
	if typ&kBubbleFar != 0 {
		if len(data) < p+4 {
			return ""
		}
		p += 4
	}
	if len(data) <= p {
		return ""
	}
	msgData := data[p:]
	if i := bytes.IndexByte(msgData, 0); i >= 0 {
		msgData = msgData[:i]
	}
	text := string(msgData)
	switch typ & kBubbleTypeMask {
	case kBubbleNormal:
		return "say: " + text
	case kBubbleWhisper:
		return "whisper: " + text
	case kBubbleYell:
		return "yell: " + text
	case kBubbleThought:
		return "think: " + text
	default:
		return text
	}
}

func decodeMessage(m []byte) string {
	if len(m) <= 16 {
		return ""
	}
	data := append([]byte(nil), m[16:]...)
	if s := decodeBEPP(data); s != "" {
		return s
	}
	if s := decodeBubble(data); s != "" {
		return s
	}
	if str := strings.TrimRight(string(data), "\x00"); str != "" {
		return str
	}

	simpleEncrypt(data)
	if s := decodeBEPP(data); s != "" {
		return s
	}
	if s := decodeBubble(data); s != "" {
		return s
	}
	if str := strings.TrimRight(string(data), "\x00"); str != "" {
		return str
	}
	return ""
}

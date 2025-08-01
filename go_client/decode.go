package main

import (
	"bytes"
	"fmt"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

func decodeMacRoman(b []byte) string {
	str, err := charmap.Macintosh.NewDecoder().Bytes(b)
	if err != nil {
		return string(b)
	}
	return string(str)
}

func decodeBEPP(data []byte) string {
	if len(data) < 3 || data[0] != 0xC2 {
		return ""
	}
	prefix := string(data[1:3])
	textBytes := data[3:]
	if i := bytes.IndexByte(textBytes, 0); i >= 0 {
		textBytes = textBytes[:i]
	}
	text := decodeMacRoman(textBytes)
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
	text := decodeMacRoman(msgData)
	switch typ & kBubbleTypeMask {
	case kBubbleNormal:
		return "say: " + text
	case kBubbleWhisper:
		return "whisper: " + text
	case kBubbleYell:
		return "yell: " + text
	case kBubbleThought:
		return "think: " + text
	case kBubblePonder:
		return "ponder: " + text
	case kBubbleNarrate:
		return "console: " + text
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
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(data) > 0 {
		txt := decodeMacRoman(data)
		if len([]rune(strings.TrimSpace(txt))) >= 4 {
			return txt
		}
	}

	simpleEncrypt(data)
	if s := decodeBEPP(data); s != "" {
		return s
	}
	if s := decodeBubble(data); s != "" {
		return s
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		data = data[:i]
	}
	if len(data) > 0 {
		txt := decodeMacRoman(data)
		if len([]rune(strings.TrimSpace(txt))) >= 4 {
			return txt
		}
	}
	return ""
}

func handleInfoText(data []byte) {
	for _, line := range bytes.Split(data, []byte{'\r'}) {
		if len(line) == 0 {
			continue
		}
		if txt := decodeBEPP(line); txt != "" {
			fmt.Println(txt)
			addMessage(txt)
			continue
		}
		if txt := decodeBubble(line); txt != "" {
			fmt.Println(txt)
			addMessage(txt)
			continue
		}
		s := strings.TrimSpace(decodeMacRoman(line))
		if s == "" || strings.HasPrefix(s, "/") {
			continue
		}
		fmt.Println(s)
		addMessage(s)
	}
}

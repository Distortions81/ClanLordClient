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
	text := strings.TrimSpace(decodeMacRoman(textBytes))
	if text == "" {
		return ""
	}

	switch prefix {
	case "th":
		if text != "" {
			return "think: " + text
		}
	case "in":
		if text != "" {
			return "info: " + text
		}
	case "sh":
		if text != "" {
			return "share: " + text
		}
	case "be":
		parseBackend(textBytes)
	}
	return ""
}

func stripBEPPTags(b []byte) []byte {
	out := b[:0]
	for i := 0; i < len(b); {
		c := b[i]
		if c == 0xC2 {
			if i+2 < len(b) {
				i += 3
				continue
			}
			break
		}
		if c >= 0x80 || c < 0x20 {
			i++
			continue
		}
		out = append(out, c)
		i++
	}
	return out
}

func decodeBubble(data []byte) (verb, text string) {
	if len(data) < 2 {
		return "", ""
	}
	typ := int(data[1])
	p := 2
	if typ&kBubbleNotCommon != 0 {
		if len(data) < p+1 {
			return "", ""
		}
		p++
	}
	if typ&kBubbleFar != 0 {
		if len(data) < p+4 {
			return "", ""
		}
		p += 4
	}
	if len(data) <= p {
		return "", ""
	}
	msgData := stripBEPPTags(data[p:])
	if i := bytes.IndexByte(msgData, 0); i >= 0 {
		msgData = msgData[:i]
	}
	lines := bytes.Split(msgData, []byte{'\r'})
	for _, ln := range lines {
		if len(ln) == 0 {
			continue
		}
		s := strings.TrimSpace(decodeMacRoman(ln))
		if s == "" {
			continue
		}
		if parseNightCommand(s) {
			continue
		}
		if text == "" {
			text = s
		} else {
			text += " " + s
		}
	}
	if text == "" {
		return "", ""
	}
	switch typ & kBubbleTypeMask {
	case kBubbleNormal:
		verb = "says"
	case kBubbleWhisper:
		verb = "whispers"
	case kBubbleYell:
		verb = "yells"
	case kBubbleThought:
		verb = "thinks"
	case kBubblePonder:
		verb = "ponders"
	case kBubbleNarrate:
		// narrate bubbles have no verb
	default:
		// unknown bubble types return no verb
	}
	return verb, text
}

func decodeMessage(m []byte) string {
	if len(m) <= 16 {
		return ""
	}
	data := append([]byte(nil), m[16:]...)
	if len(data) > 0 && data[0] == 0xC2 {
		if s := decodeBEPP(data); s != "" {
			return s
		}
		return ""
	}
	if _, s := decodeBubble(data); s != "" {
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
	if len(data) > 0 && data[0] == 0xC2 {
		if s := decodeBEPP(data); s != "" {
			return s
		}
		return ""
	}
	if _, s := decodeBubble(data); s != "" {
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
		if line[0] == 0xC2 {
			if txt := decodeBEPP(line); txt != "" {
				fmt.Println(txt)
				addMessage(txt)
			}
			continue
		}
		if _, txt := decodeBubble(line); txt != "" {
			fmt.Println(txt)
			addMessage(txt)
			continue
		}
		s := strings.TrimSpace(decodeMacRoman(stripBEPPTags(line)))
		if s == "" {
			continue
		}
		if parseNightCommand(s) {
			continue
		}
		if strings.HasPrefix(s, "/") {
			continue
		}
		fmt.Println(s)
		addMessage(s)
	}
}

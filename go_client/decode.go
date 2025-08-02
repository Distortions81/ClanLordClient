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

func decodeBEPP(data []byte) (string, MsgClass) {
        if len(data) < 3 || data[0] != 0xC2 {
                return "", MsgDefault
        }
        prefix := string(data[1:3])
        textBytes := data[3:]
        if i := bytes.IndexByte(textBytes, 0); i >= 0 {
                textBytes = textBytes[:i]
        }
        if prefix == "be" {
                parseBackend(textBytes)
                return "", MsgDefault
        }
        cleaned, you := stripBEPPTags(textBytes)
        text := strings.TrimSpace(decodeMacRoman(cleaned))
        if text == "" {
                return "", MsgDefault
        }
        class := MsgDefault
        switch prefix {
        case "th":
                class = MsgInfo
                if you {
                        class = MsgMySpeech
                        text = "You think: " + text
                } else {
                        text = "think: " + text
                }
        case "in":
                class = MsgInfo
                text = "info: " + text
        case "sh":
                class = MsgShare
                if you {
                        class = MsgMySpeech
                        text = "You share: " + text
                } else {
                        text = "share: " + text
                }
        case "lg":
                class = MsgLogon
        case "lf":
                class = MsgLogoff
        case "er":
                class = MsgError
        default:
                class = MsgDefault
        }
        return text, class
}

func stripBEPPTags(b []byte) ([]byte, bool) {
        out := b[:0]
        you := false
        for i := 0; i < len(b); {
                c := b[i]
                if c == 0xC2 {
                        if i+2 >= len(b) {
                                break
                        }
                        tag := string(b[i+1 : i+3])
                        i += 3
                        if tag == "pn" {
                                end := bytes.Index(b[i:], []byte{0xC2, 'p', 'n'})
                                if end < 0 {
                                        continue
                                }
                                name := strings.TrimSpace(decodeMacRoman(b[i : i+end]))
                                if strings.EqualFold(name, playerName) {
                                        out = append(out, []byte("You")...)
                                        you = true
                                } else {
                                        out = append(out, []byte(name)...)
                                }
                                i += end + 3
                        }
                        continue
                }
                if c >= 0x80 || c < 0x20 {
                        i++
                        continue
                }
                out = append(out, c)
                i++
        }
        return out, you
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
        msgData, _ := stripBEPPTags(data[p:])
	if i := bytes.IndexByte(msgData, 0); i >= 0 {
		msgData = msgData[:i]
	}
	lines := bytes.Split(msgData, []byte{'\r'})
	var text string
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
		return ""
	}
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
        if len(data) > 0 && data[0] == 0xC2 {
                if s, _ := decodeBEPP(data); s != "" {
                        return s
                }
                return ""
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
        if len(data) > 0 && data[0] == 0xC2 {
                if s, _ := decodeBEPP(data); s != "" {
                        return s
                }
                return ""
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
                if line[0] == 0xC2 {
                        if txt, class := decodeBEPP(line); txt != "" {
                                fmt.Println(txt)
                                addMessage(class, txt)
                        }
                        continue
                }
                if txt := decodeBubble(line); txt != "" {
                        fmt.Println(txt)
                        addMessage(MsgDefault, txt)
                        continue
                }
                sBytes, _ := stripBEPPTags(line)
                s := strings.TrimSpace(decodeMacRoman(sBytes))
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
                addMessage(MsgDefault, s)
        }
}

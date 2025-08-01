package main

import (
	"bufio"
	"crypto/aes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"golang.org/x/text/encoding/charmap"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

func simpleEncrypt(data []byte) {
	key := []byte{0x3c, 0x5a, 0x69, 0x93, 0xa5, 0xc6}
	j := 0
	for i := range data {
		data[i] ^= key[j]
		j++
		if j == len(key) {
			j = 0
		}
	}
}

func encodeMacRoman(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if b, ok := charmap.Macintosh.EncodeRune(r); ok {
			out = append(out, b)
		} else {
			out = append(out, '?')
		}
	}
	return out
}

func encodeFullVersion(v int) uint32 { return uint32(v) << 8 }

const (
	baseVersion = 1353
	kDescPlayer = 1
)

func hexDump(prefix string, data []byte) {
	if !debug {
		return
	}
	log.Printf("%s %d bytes\n%s", prefix, len(data), hex.Dump(data))
}

func dlog(format string, args ...interface{}) {
	if debug {
		log.Printf(format, args...)
	}
}

const (
	kTypeVersion = 0x56657273 // 'Vers'
)

var errorNames = map[int16]string{
	-30972: "kDownloadNewVersionLive",
	-30973: "kDownloadNewVersionTest",
	-30999: "kBadCharName",
	-30998: "kBadCharPass",
	-30996: "kIncompatibleVersions",
	-30992: "kShuttingDown",
	-30991: "kGameNotOpen",
	-30988: "kBadAcctName",
	-30987: "kBadAcctPass",
	-30985: "kNoFreeSlot",
	-30984: "kBadAcctChar",
	-30981: "kCharOnline",
}

func loadAdditionalErrorNames() {
	path := filepath.Join("..", "mac_client", "client", "public", "Public_cl.h")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	re := regexp.MustCompile(`\s*(k\w+)\s*=\s*(-?\d+),`)
	for scanner.Scan() {
		line := scanner.Text()
		if m := re.FindStringSubmatch(line); m != nil {
			val, err := strconv.Atoi(m[2])
			if err == nil {
				if _, ok := errorNames[int16(val)]; !ok {
					errorNames[int16(val)] = m[1]
				}
			}
		}
	}
}

func init() { loadAdditionalErrorNames() }

var debug bool = true
var logFile *os.File
var ackFrame int32
var resendFrame int32
var commandNum uint32 = 1
var playerName string
var playerIndex uint8 = 0xff

const (
	kBubbleNormal       = 0
	kBubbleWhisper      = 1
	kBubbleYell         = 2
	kBubbleThought      = 3
	kBubbleRealAction   = 4
	kBubbleMonster      = 5
	kBubblePlayerAction = 6
	kBubblePonder       = 7
	kBubbleNarrate      = 8

	kBubbleTypeMask  = 0x3F
	kBubbleNotCommon = 0x40
	kBubbleFar       = 0x80
)

const kPIMDownField = 0x0001 // mouse down; player wants to move

// illumination flags from Public_cl.h
const (
	kLightAdjust25Pct  = 1 << 0
	kLightAdjust50Pct  = 1 << 1
	kLightAreaIsDarker = 1 << 2
	kLightNoNightMods  = 1 << 3
	kLightNoShadows    = 1 << 4
	kLightForce100Pct  = 1 << 5
)

// inventory command values from Public_cl.h
const (
	kInvCmdNone = iota
	kInvCmdFull
	kInvCmdAdd
	kInvCmdAddEquip
	kInvCmdDelete
	kInvCmdEquip
	kInvCmdUnequip
	kInvCmdMultiple
	kInvCmdName

	kInvCmdIndex = 0x80
)

// item slots from Public_cl.h
const (
	kItemSlotNotInventory = iota
	kItemSlotNotWearable
	kItemSlotForehead
	kItemSlotNeck
	kItemSlotShoulder
	kItemSlotArms
	kItemSlotGloves
	kItemSlotFinger
	kItemSlotCoat
	kItemSlotCloak
	kItemSlotTorso
	kItemSlotWaist
	kItemSlotLegs
	kItemSlotFeet
	kItemSlotRightHand
	kItemSlotLeftHand
	kItemSlotBothHands
	kItemSlotHead

	kItemSlotFirstReal = kItemSlotForehead
	kItemSlotLastReal  = kItemSlotHead
)

const beppChar = "\302"

func readKeyFileVersion(path string) (uint32, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil {
		return 0, err
	}
	count := int(binary.BigEndian.Uint32(header[2:6]))

	entry := make([]byte, 16)
	for i := 0; i < count; i++ {
		if _, err := io.ReadFull(f, entry); err != nil {
			return 0, err
		}
		pos := binary.BigEndian.Uint32(entry[0:4])
		size := binary.BigEndian.Uint32(entry[4:8])
		typ := binary.BigEndian.Uint32(entry[8:12])
		id := binary.BigEndian.Uint32(entry[12:16])
		if typ == kTypeVersion && id == 0 {
			if _, err := f.Seek(int64(pos), io.SeekStart); err != nil {
				return 0, err
			}
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return 0, err
			}
			v := binary.BigEndian.Uint32(buf)
			if v <= 0xFF {
				v <<= 8
			}
			return v, nil
		}
	}
	return 0, fmt.Errorf("version record not found in %s", path)
}

func answerChallenge(password string, challenge []byte) ([]byte, error) {
	key := md5.Sum([]byte(password))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	if len(challenge)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid challenge length")
	}
	plain := make([]byte, len(challenge))
	for i := 0; i < len(challenge); i += aes.BlockSize {
		block.Decrypt(plain[i:i+aes.BlockSize], challenge[i:i+aes.BlockSize])
	}
	h := md5.Sum(plain)
	encoded := make([]byte, len(h))
	for i := 0; i < len(h); i += aes.BlockSize {
		block.Encrypt(encoded[i:i+aes.BlockSize], h[i:i+aes.BlockSize])
	}
	return encoded, nil
}

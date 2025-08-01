package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var mouseX, mouseY = uint16(rand.Intn(1600)), uint16(rand.Intn(1200))

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

func encodeFullVersion(v int) uint32 {
	return uint32(v) << 8
}

const baseVersion = 1353

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

// loadAdditionalErrorNames parses Public_cl.h to populate errorNames.
func loadAdditionalErrorNames() {
	path := filepath.Join("..", "mac_client", "client", "public", "Public_cl.h")
	f, err := os.Open(path)
	if err != nil {
		// ignore errors; table will remain partial
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

func init() {
	loadAdditionalErrorNames()
}

var debug bool = true
var logFile *os.File
var ackFrame int32
var resendFrame int32
var commandNum uint32 = 1

// bubble speech types from Public_cl.h
const (
	kBubbleNormal  = 0
	kBubbleWhisper = 1
	kBubbleYell    = 2
	kBubbleThought = 3

	kBubbleTypeMask  = 0x3F
	kBubbleNotCommon = 0x40
	kBubbleFar       = 0x80
)

const beppChar = "\302"

// readKeyFileVersion reads the kTypeVersion record from a DTS key file.
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

func sendIdentifiers(conn net.Conn, clientVersion, imagesVersion, soundsVersion uint32) error {
	const kMsgIdentifiers = 19
	uname := os.Getenv("USER")
	if uname == "" {
		uname = "unknown"
	}
	hname, _ := os.Hostname()
	if hname == "" {
		hname = "unknown"
	}
	boot := "/"

	data := make([]byte, 0, 8+6+len(uname)+1+len(hname)+1+len(boot)+1+1)
	data = append(data, make([]byte, 8)...) // magic file info placeholder
	data = append(data, make([]byte, 6)...) // ethernet address placeholder
	data = append(data, []byte(uname)...)
	data = append(data, 0)
	data = append(data, []byte(hname)...)
	data = append(data, 0)
	data = append(data, []byte(boot)...)
	data = append(data, 0)
	data = append(data, byte(0)) // language

	buf := make([]byte, 16+len(data))
	binary.BigEndian.PutUint16(buf[0:2], kMsgIdentifiers)
	binary.BigEndian.PutUint16(buf[2:4], 0)
	binary.BigEndian.PutUint32(buf[4:8], clientVersion)
	binary.BigEndian.PutUint32(buf[8:12], imagesVersion)
	binary.BigEndian.PutUint32(buf[12:16], soundsVersion)
	copy(buf[16:], data)
	simpleEncrypt(buf[16:])
	dlog("identifiers client=%d images=%d sounds=%d", clientVersion, imagesVersion, soundsVersion)
	return sendMessage(conn, buf)
}

func sendMessage(conn net.Conn, msg []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(msg)))
	if _, err := conn.Write(size[:]); err != nil {
		return err
	}
	_, err := conn.Write(msg)
	tag := binary.BigEndian.Uint16(msg[:2])
	dlog("send tcp tag %d len %d", tag, len(msg))
	hexDump("send", msg)
	return err
}

func sendUDPMessage(conn net.Conn, msg []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(msg)))
	buf := append(size[:], msg...)
	_, err := conn.Write(buf)
	tag := binary.BigEndian.Uint16(msg[:2])
	dlog("send udp tag %d len %d", tag, len(msg))
	hexDump("send", msg)
	return err
}

func readUDPMessage(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 65535)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	if n < 2 {
		return nil, fmt.Errorf("short udp packet")
	}
	sz := int(binary.BigEndian.Uint16(buf[:2]))
	if sz > n-2 {
		return nil, fmt.Errorf("incomplete udp packet")
	}
	msg := append([]byte(nil), buf[2:2+sz]...)
	tag := binary.BigEndian.Uint16(msg[:2])
	dlog("recv udp tag %d len %d", tag, len(msg))
	hexDump("recv", msg)
	return msg, nil
}

func sendPlayerInput(conn net.Conn) error {
	const kMsgPlayerInput = 3
	buf := make([]byte, 20+1)
	binary.BigEndian.PutUint16(buf[0:2], kMsgPlayerInput)
	binary.BigEndian.PutUint16(buf[2:4], mouseX) // mouse H
	binary.BigEndian.PutUint16(buf[4:6], mouseY) // mouse V
	binary.BigEndian.PutUint16(buf[6:8], 0)      // flags
	binary.BigEndian.PutUint32(buf[8:12], uint32(ackFrame))
	binary.BigEndian.PutUint32(buf[12:16], uint32(resendFrame))
	binary.BigEndian.PutUint32(buf[16:20], commandNum)
	buf[20] = 0
	commandNum++
	dlog("player input ack=%d resend=%d cmd=%d mouse=%d,%d", ackFrame, resendFrame, commandNum-1, mouseX, mouseY)
	return sendUDPMessage(conn, buf)
}

func readMessage(conn net.Conn) ([]byte, error) {
	var sizeBuf [2]byte
	if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
		return nil, err
	}
	sz := binary.BigEndian.Uint16(sizeBuf[:])
	buf := make([]byte, sz)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return nil, err
	}
	tag := binary.BigEndian.Uint16(buf[:2])
	dlog("recv tcp tag %d len %d", tag, len(buf))
	hexDump("recv", buf)
	return buf, nil
}

func downloadGZ(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, gz); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

func downloadTGZ(url, destDir string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		path := filepath.Join(destDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
				return err
			}
			f, err := os.Create(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

func autoUpdate(resp []byte) error {
	if len(resp) < 16 {
		return fmt.Errorf("short response for update")
	}
	base := string(resp[16:])
	if i := strings.IndexByte(base, 0); i >= 0 {
		base = base[:i]
	}
	base = strings.TrimRight(base, "/")
	clientVer := binary.BigEndian.Uint32(resp[4:8])
	fmt.Printf("Client version: %v\n", clientVer)
	imgVer := binary.BigEndian.Uint32(resp[8:12])
	sndVer := binary.BigEndian.Uint32(resp[12:16])
	/*
	*maj := clientVer >> 8
	*min := clientVer & 0xFF
	*} else {
	*	clientURL = fmt.Sprintf("%s/mac/ClanLord.%d.%d.tgz", base, maj, min)
	*}
	 */
	imgURL := fmt.Sprintf("%s/data/CL_Images.%d.gz", base, imgVer>>8)
	sndURL := fmt.Sprintf("%s/data/CL_Sounds.%d.gz", base, sndVer>>8)
	/*
	*if err := os.MkdirAll("updates/client", 0755); err != nil {
	*	return err
	*}
	*fmt.Println("downloading", clientURL)
	*if err := downloadTGZ(clientURL, "updates/client"); err != nil {
	*	return err
	*}
	 */
	fmt.Println("downloading", imgURL)
	if err := downloadGZ(imgURL, "CL_Images"); err != nil {
		return err
	}
	fmt.Println("downloading", sndURL)
	if err := downloadGZ(sndURL, "CL_Sounds"); err != nil {
		return err
	}
	return nil
}

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

type frameDescriptor struct {
	Index  uint8
	Type   uint8
	PictID uint16
	Name   string
	Colors []byte
}

type framePicture struct {
	PictID uint16
	H, V   int16
}

type frameMobile struct {
	Index  uint8
	State  uint16
	V, H   int16
	Colors uint8
}

// bitReader helps decode the packed picture fields.
type bitReader struct {
	data   []byte
	bitPos int
}

func (br *bitReader) readBits(n int) uint32 {
	var v uint32
	for n > 0 {
		if br.bitPos/8 >= len(br.data) {
			return v
		}
		b := br.data[br.bitPos/8]
		remain := 8 - br.bitPos%8
		take := remain
		if take > n {
			take = n
		}
		shift := remain - take
		v = (v << take) | uint32((b>>shift)&((1<<take)-1))
		br.bitPos += take
		n -= take
	}
	return v
}

func signExtend(v uint32, bits int) int16 {
	if v&(1<<(bits-1)) != 0 {
		v |= ^uint32(0) << bits
	}
	return int16(int32(v))
}

// handleDrawState decodes the packed draw state message. Only a subset of the
// data is currently used by the client, but parsing everything helps with
// debugging and future work.
func handleDrawState(m []byte) {
	if len(m) < 25 { // 16 byte header + 9 bytes minimum
		return
	}
	data := append([]byte(nil), m[16:]...)
	simpleEncrypt(data)
	if len(data) < 9 {
		return
	}

	ackCmd := data[0]
	ackFrame = int32(binary.BigEndian.Uint32(data[1:5]))
	resendFrame = int32(binary.BigEndian.Uint32(data[5:9]))
	p := 9

	if len(data) <= p {
		return
	}
	descCount := int(data[p])
	p++
	descs := make([]frameDescriptor, 0, descCount)
	for i := 0; i < descCount && p < len(data); i++ {
		if p+4 > len(data) {
			return
		}
		d := frameDescriptor{}
		d.Index = data[p]
		d.Type = data[p+1]
		d.PictID = binary.BigEndian.Uint16(data[p+2:])
		p += 4
		if idx := bytes.IndexByte(data[p:], 0); idx >= 0 {
			d.Name = string(data[p : p+idx])
			p += idx + 1
		} else {
			return
		}
		if p >= len(data) {
			return
		}
		cnt := int(data[p])
		p++
		if p+cnt > len(data) {
			return
		}
		d.Colors = append([]byte(nil), data[p:p+cnt]...)
		p += cnt
		descs = append(descs, d)
	}

	if len(data) < p+7 {
		return
	}
	hp := data[p]
	hpMax := data[p+1]
	sp := data[p+2]
	spMax := data[p+3]
	bal := data[p+4]
	balMax := data[p+5]
	light := data[p+6]
	p += 7

	if len(data) <= p {
		return
	}
	pictCount := int(data[p])
	p++
	pictAgain := 0
	if pictCount == 255 {
		if len(data) < p+2 {
			return
		}
		pictAgain = int(data[p])
		pictCount = int(data[p+1])
		p += 2
	}

	pics := make([]framePicture, 0, pictAgain+pictCount)
	br := bitReader{data: data[p:]}
	for i := 0; i < pictCount; i++ {
		id := uint16(br.readBits(14))
		h := signExtend(br.readBits(11), 11)
		v := signExtend(br.readBits(11), 11)
		pics = append(pics, framePicture{PictID: id, H: h, V: v})
	}
	// move past bit-packed picture data
	p += br.bitPos / 8
	if br.bitPos%8 != 0 {
		p++
	}

	if len(data) <= p {
		return
	}
	mobileCount := int(data[p])
	p++
	mobiles := make([]frameMobile, 0, mobileCount)
	for i := 0; i < mobileCount && p+8 <= len(data); i++ {
		m := frameMobile{}
		m.Index = data[p]
		m.State = binary.BigEndian.Uint16(data[p+1:])
		m.V = int16(binary.BigEndian.Uint16(data[p+3:]))
		m.H = int16(binary.BigEndian.Uint16(data[p+5:]))
		m.Colors = data[p+7]
		p += 8
		mobiles = append(mobiles, m)
	}

	stateData := data[p:]

	dlog("draw state cmd=%d ack=%d resend=%d desc=%d pict=%d again=%d mobile=%d state=%d", ackCmd, ackFrame, resendFrame, len(descs), len(pics), pictAgain, len(mobiles), len(stateData))

	// try to decode any embedded text in the state data
	if txt := decodeBEPP(stateData); txt != "" {
		fmt.Println(txt)
	} else if txt := decodeBubble(stateData); txt != "" {
		fmt.Println(txt)
	}
}

func decodeMessage(m []byte) string {
	if len(m) <= 16 {
		return ""
	}
	data := append([]byte(nil), m[16:]...)

	// Many server messages arrive without encryption while others use the
	// simple XOR cipher. Try decoding the plain data first and fall back to
	// the encrypted form when that yields no text. This allows us to handle
	// both cases transparently.
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

func requestCharList(conn net.Conn, account, password string, challenge []byte, clientVersion, imagesVersion, soundsVersion uint32) ([]string, error) {
	answer, err := answerChallenge(password, challenge)
	if err != nil {
		return nil, err
	}

	const kMsgCharList = 14
	buf := make([]byte, 16+len(account)+1+len(answer))
	binary.BigEndian.PutUint16(buf[0:2], kMsgCharList)
	binary.BigEndian.PutUint16(buf[2:4], 0)
	binary.BigEndian.PutUint32(buf[4:8], clientVersion)
	binary.BigEndian.PutUint32(buf[8:12], imagesVersion)
	binary.BigEndian.PutUint32(buf[12:16], soundsVersion)
	copy(buf[16:], []byte(account))
	buf[16+len(account)] = 0
	copy(buf[17+len(account):], answer)
	simpleEncrypt(buf[16:])
	if err := sendMessage(conn, buf); err != nil {
		return nil, err
	}
	dlog("request character list for %s", account)

	resp, err := readMessage(conn)
	if err != nil {
		return nil, err
	}
	if len(resp) < 28 {
		return nil, fmt.Errorf("short char list resp")
	}
	resTag := binary.BigEndian.Uint16(resp[:2])
	if resTag != kMsgCharList {
		return nil, fmt.Errorf("unexpected tag %d", resTag)
	}
	result := int16(binary.BigEndian.Uint16(resp[2:4]))
	simpleEncrypt(resp[16:])
	if result != 0 {
		return nil, fmt.Errorf("server result %d", result)
	}

	data := resp[16:]
	_ = binary.BigEndian.Uint32(data[0:4])  // status
	_ = binary.BigEndian.Uint32(data[4:8])  // paid up
	_ = binary.BigEndian.Uint32(data[8:12]) // max chars

	namesData := data[12:]
	var names []string
	for len(namesData) > 0 {
		i := bytes.IndexByte(namesData, 0)
		if i < 0 {
			break
		}
		if i == 0 {
			break
		}
		names = append(names, string(namesData[:i]))
		namesData = namesData[i+1:]
	}
	dlog("server returned %d characters", len(names))
	return names, nil
}

func main() {
	host := flag.String("host", "server.deltatao.com:5010", "server address")
	name := flag.String("name", "demo", "character name")
	pass := flag.String("pass", "demo", "character password")
	clientVer := flag.Int("client-version", 1440, "client version number (kVersionNumber)")
	flag.BoolVar(&debug, "debug", true, "enable debug logging")
	flag.Parse()

	if debug {
		logName := fmt.Sprintf("debug-%s.log", time.Now().Format("20060102-150405"))
		f, err := os.Create(logName)
		if err == nil {
			logFile = f
			log.SetOutput(f)
			defer f.Close()
		} else {
			fmt.Printf("warning: could not create log file: %v\n", err)
		}
	} else {
		log.SetOutput(io.Discard)
	}

	autoDemo := *name == "demo" && *pass == "demo"

	// clientVersion corresponds to kVersionNumber from
	// VersionNumber_cl.h in the C client. The server currently
	// expects version 1440 with sub-version 0.
	clientVersion := *clientVer
	for {
		imagesVersion, err := readKeyFileVersion("CL_Images")
		imagesMissing := false
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("CL_Images missing; will fetch from server")
				imagesVersion = 0
				imagesMissing = true
			} else {
				log.Printf("warning: %v", err)
				imagesVersion = encodeFullVersion(clientVersion)
			}
		}
		soundsVersion, err := readKeyFileVersion("CL_Sounds")
		soundsMissing := false
		if err != nil {
			if os.IsNotExist(err) {
				log.Printf("CL_Sounds missing; will fetch from server")
				soundsVersion = 0
				soundsMissing = true
			} else {
				log.Printf("warning: %v", err)
				soundsVersion = encodeFullVersion(clientVersion)
			}
		}

		sendVersion := clientVersion
		if imagesMissing || soundsMissing {
			sendVersion = baseVersion - 1
		}

		tcpConn, err := net.Dial("tcp", *host)
		if err != nil {
			log.Fatalf("tcp connect: %v", err)
		}
		udpConn, err := net.Dial("udp", *host)
		if err != nil {
			log.Fatalf("udp connect: %v", err)
		}

		var idBuf [4]byte
		if _, err := io.ReadFull(tcpConn, idBuf[:]); err != nil {
			log.Fatalf("read id: %v", err)
		}

		handshake := append([]byte{0xff, 0xff}, idBuf[:]...)
		if _, err := udpConn.Write(handshake); err != nil {
			log.Fatalf("send handshake: %v", err)
		}

		var confirm [2]byte
		if _, err := io.ReadFull(tcpConn, confirm[:]); err != nil {
			log.Fatalf("confirm handshake: %v", err)
		}
		if err := sendIdentifiers(tcpConn, encodeFullVersion(sendVersion), imagesVersion, soundsVersion); err != nil {
			log.Fatalf("send identifiers: %v", err)
		}
		fmt.Println("connected to", *host)

		msg, err := readMessage(tcpConn)
		if err != nil {
			log.Fatalf("read challenge: %v", err)
		}
		if len(msg) < 16 {
			log.Fatalf("short challenge message")
		}
		tag := binary.BigEndian.Uint16(msg[:2])
		const kMsgChallenge = 18
		if tag != kMsgChallenge {
			log.Fatalf("unexpected msg tag %d", tag)
		}
		challenge := msg[8 : 8+16]

		if autoDemo {
			names, err := requestCharList(tcpConn, "demo", "demo", challenge, encodeFullVersion(sendVersion), imagesVersion, soundsVersion)
			if err != nil {
				log.Fatalf("list demo: %v", err)
			}
			if len(names) == 0 {
				log.Fatalf("no demo characters available")
			}
			rand.Seed(time.Now().UnixNano())
			*name = names[rand.Intn(len(names))]
			fmt.Println("selected demo character:", *name)
		}

		answer, err := answerChallenge(*pass, challenge)
		if err != nil {
			log.Fatalf("hash: %v", err)
		}

		const kMsgLogOn = 13
		buf := make([]byte, 16+len(*name)+1+len(answer))
		binary.BigEndian.PutUint16(buf[0:2], kMsgLogOn)
		binary.BigEndian.PutUint16(buf[2:4], 0)
		binary.BigEndian.PutUint32(buf[4:8], encodeFullVersion(sendVersion))
		binary.BigEndian.PutUint32(buf[8:12], imagesVersion)
		binary.BigEndian.PutUint32(buf[12:16], soundsVersion)
		copy(buf[16:], []byte(*name))
		buf[16+len(*name)] = 0
		copy(buf[17+len(*name):], answer)
		simpleEncrypt(buf[16:])

		if err := sendMessage(tcpConn, buf); err != nil {
			log.Fatalf("send login: %v", err)
		}

		resp, err := readMessage(tcpConn)
		if err != nil {
			log.Fatalf("read login response: %v", err)
		}
		resTag := binary.BigEndian.Uint16(resp[:2])
		const kMsgLogOnResp = 13
		if resTag != kMsgLogOnResp {
			log.Fatalf("unexpected response tag %d", resTag)
		}
		result := int16(binary.BigEndian.Uint16(resp[2:4]))
		if name, ok := errorNames[result]; ok && result != 0 {
			fmt.Printf("login result: %d (%s)\n", result, name)
		} else {
			fmt.Printf("login result: %d\n", result)
		}

		if result == -30972 || result == -30973 {
			fmt.Println("server requested update, downloading...")
			if err := autoUpdate(resp); err != nil {
				log.Fatalf("auto update: %v", err)
			}
			fmt.Println("update complete, reconnecting...")
			tcpConn.Close()
			udpConn.Close()
			continue
		}

		if result == 0 {
			fmt.Println("login succeeded, reading messages (Ctrl-C to quit)...")
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := sendPlayerInput(udpConn); err != nil {
				fmt.Printf("send player input: %v\n", err)
			}

			go func() {
				ticker := time.NewTicker(2 * time.Second)
				defer ticker.Stop()
				for {
					if err := sendPlayerInput(udpConn); err != nil {
						fmt.Printf("send player input: %v\n", err)
					}
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
					}
				}
			}()

			go func() {
				for {
					if err := udpConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
						fmt.Printf("udp deadline: %v\n", err)
						return
					}
					m, err := readUDPMessage(udpConn)
					if err != nil {
						if ne, ok := err.(net.Error); ok && ne.Timeout() {
							select {
							case <-ctx.Done():
								return
							default:
								continue
							}
						}
						fmt.Printf("udp read error: %v\n", err)
						return
					}
					tag := binary.BigEndian.Uint16(m[:2])
					if tag == 2 { // kMsgDrawState
						handleDrawState(m)
					}
					if txt := decodeMessage(m); txt != "" {
						fmt.Println(txt)
					} else {
						fmt.Printf("udp msg tag %d len %d\n", tag, len(m))
					}
				}
			}()

		loop:
			for {
				if err := tcpConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					fmt.Printf("set read deadline: %v\n", err)
					break
				}
				m, err := readMessage(tcpConn)
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						select {
						case <-ctx.Done():
							break loop
						default:
							continue
						}
					}
					fmt.Printf("read error: %v\n", err)
					break
				}
				tag := binary.BigEndian.Uint16(m[:2])
				if tag == 2 { // kMsgDrawState
					handleDrawState(m)
				}
				if txt := decodeMessage(m); txt != "" {
					fmt.Println(txt)
				} else {
					fmt.Printf("msg tag %d len %d\n", tag, len(m))
				}
				select {
				case <-ctx.Done():
					break loop
				default:
				}
			}
			tcpConn.Close()
			udpConn.Close()
		}
		break
	}
}

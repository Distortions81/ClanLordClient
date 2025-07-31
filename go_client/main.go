package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
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

func hexDump(prefix string, data []byte) {
	if !dumpTraffic {
		return
	}
	fmt.Printf("%s %d bytes\n", prefix, len(data))
	fmt.Println(hex.Dump(data))
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

var dumpTraffic bool

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
	return sendMessage(conn, buf)
}

func sendMessage(conn net.Conn, msg []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(msg)))
	if _, err := conn.Write(size[:]); err != nil {
		return err
	}
	_, err := conn.Write(msg)
	hexDump("send", msg)
	return err
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
	imgVer := binary.BigEndian.Uint32(resp[8:12])
	sndVer := binary.BigEndian.Uint32(resp[12:16])
	maj := clientVer >> 8
	min := clientVer & 0xFF
	var clientURL string
	if min == 0 {
		clientURL = fmt.Sprintf("%s/mac/ClanLord.%d.tgz", base, maj)
	} else {
		clientURL = fmt.Sprintf("%s/mac/ClanLord.%d.%d.tgz", base, maj, min)
	}
	imgURL := fmt.Sprintf("%s/data/CL_Images.%d.gz", base, imgVer>>8)
	sndURL := fmt.Sprintf("%s/data/CL_Sounds.%d.gz", base, sndVer>>8)
	if err := os.MkdirAll("updates/client", 0755); err != nil {
		return err
	}
	fmt.Println("downloading", clientURL)
	if err := downloadTGZ(clientURL, "updates/client"); err != nil {
		return err
	}
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
	return names, nil
}

func main() {
	host := flag.String("host", "server.deltatao.com:5010", "server address")
	name := flag.String("name", "demo", "character name")
	pass := flag.String("pass", "demo", "character password")
	listDemo := flag.Bool("list-demo", false, "list available demo characters")
	flag.BoolVar(&dumpTraffic, "dump", false, "dump raw network traffic")
	flag.Parse()

	// clientVersion corresponds to kFullVersionNumber from
	// VersionNumber_cl.h in the C client. The server currently
	// expects version 1440 with sub-version 0, so send that
	//  number shifted left by 8 bits.
	const clientVersion = 368640
	for {
		imagesVersion, err := readKeyFileVersion("CL_Images")
		if err != nil {
			log.Printf("warning: %v", err)
			imagesVersion = clientVersion
		}
		soundsVersion, err := readKeyFileVersion("CL_Sounds")
		if err != nil {
			log.Printf("warning: %v", err)
			soundsVersion = clientVersion
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
		if err := sendIdentifiers(tcpConn, clientVersion, imagesVersion, soundsVersion); err != nil {
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

		if *listDemo {
			names, err := requestCharList(tcpConn, "demo", "demo", challenge, clientVersion, imagesVersion, soundsVersion)
			if err != nil {
				log.Fatalf("list demo: %v", err)
			}
			for _, n := range names {
				fmt.Println(n)
			}
			return
		}

		answer, err := answerChallenge(*pass, challenge)
		if err != nil {
			log.Fatalf("hash: %v", err)
		}

		const kMsgLogOn = 13
		buf := make([]byte, 16+len(*name)+1+len(answer))
		binary.BigEndian.PutUint16(buf[0:2], kMsgLogOn)
		binary.BigEndian.PutUint16(buf[2:4], 0)
		binary.BigEndian.PutUint32(buf[4:8], clientVersion)
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

		tcpConn.Close()
		udpConn.Close()

		if result == -30972 || result == -30973 {
			fmt.Println("server requested update, downloading...")
			if err := autoUpdate(resp); err != nil {
				log.Fatalf("auto update: %v", err)
			}
			fmt.Println("update complete, reconnecting...")
			continue
		}

		if result == 0 {
			tcpConn2, err := net.Dial("tcp", *host)
			if err != nil {
				log.Fatalf("tcp reconnect: %v", err)
			}
			udpConn2, err := net.Dial("udp", *host)
			if err != nil {
				log.Fatalf("udp reconnect: %v", err)
			}

			if _, err := io.ReadFull(tcpConn2, idBuf[:]); err != nil {
				log.Fatalf("read id: %v", err)
			}
			handshake := append([]byte{0xff, 0xff}, idBuf[:]...)
			if _, err := udpConn2.Write(handshake); err != nil {
				log.Fatalf("send handshake: %v", err)
			}
			if _, err := io.ReadFull(tcpConn2, confirm[:]); err != nil {
				log.Fatalf("confirm handshake: %v", err)
			}
			if err := sendIdentifiers(tcpConn2, clientVersion, imagesVersion, soundsVersion); err != nil {
				log.Fatalf("send identifiers: %v", err)
			}

			fmt.Println("login succeeded, reading messages (Ctrl-C to quit)...")
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		loop:
			for {
				select {
				case <-sig:
					break loop
				default:
					m, err := readMessage(tcpConn2)
					if err != nil {
						fmt.Printf("read error: %v\n", err)
						break loop
					}
					fmt.Printf("msg tag %d len %d\n", binary.BigEndian.Uint16(m[:2]), len(m))
				}
			}
			tcpConn2.Close()
			udpConn2.Close()
		}
		break
	}
}

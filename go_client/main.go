package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"go_client/climg"
)

func main() {
	host := flag.String("host", "server.deltatao.com:5010", "server address")
	name := flag.String("name", "demo", "character name")
	account := flag.String("account", "", "account name")
	accountPass := flag.String("account-pass", "", "account password (for character list)")
	pass := flag.String("pass", "demo", "character password")
	clmov := flag.String("clmov", "", "play back a .clMov file")
	flag.IntVar(&scale, "scale", 2, "image upscaling")
	flag.BoolVar(&interp, "smooth", true, "motion smoothing (linear interpolation)")
	flag.BoolVar(&linear, "filter", false, "image filtering (bilinear)")
	flag.BoolVar(&onion, "blend", false, "frame blending (smoother animations)")
	clientVer := flag.Int("client-version", 1440, "client version number (for testing)")
	flag.BoolVar(&debug, "debug", false, "verbose/debug logging")

	flag.Parse()

	nameProvided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "name" {
			nameProvided = true
		}
	})

	if linear {
		drawFilter = ebiten.FilterLinear
	}

	var imgErr error
	clImages, imgErr = climg.Load("CL_Images")
	if imgErr != nil {
		log.Printf("load CL_Images: %v", imgErr)
		addMessage(fmt.Sprintf("load CL_Images: %v", imgErr))
	}
	if imgErr != nil && *clmov != "" {
		alt := filepath.Join(filepath.Dir(*clmov), "CL_Images")
		if imgs, err := climg.Load(alt); err == nil {
			clImages = imgs
			imgErr = nil
			log.Printf("loaded CL_Images from %s", alt)
		} else {
			log.Printf("load CL_Images from %s: %v", alt, err)
			addMessage(fmt.Sprintf("load CL_Images from %s: %v", alt, err))
		}
	}

	if *clmov != "" {
		frames, err := parseMovie(*clmov, *clientVer)
		if err != nil {
			log.Fatalf("parse movie: %v", err)
		}

		playerName = extractMoviePlayerName(frames)

		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for _, m := range frames {
				if len(m) >= 2 && binary.BigEndian.Uint16(m[:2]) == 2 {
					handleDrawState(m)
				}
				if txt := decodeMessage(m); txt != "" {
					//fmt.Println(txt)
				}
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return
				}
			}
			cancel()
		}()

		runGame(ctx)
		return
	}

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

		tcpConn, err = net.Dial("tcp", *host)
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
		if err := sendClientIdentifiers(tcpConn, encodeFullVersion(sendVersion), imagesVersion, soundsVersion); err != nil {
			log.Fatalf("send identifiers: %v", err)
		}
		fmt.Println("connected to", *host)

		msg, err := readTCPMessage(tcpConn)
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

		if *account != "" {
			names, err := requestCharList(tcpConn, *account, *accountPass, challenge, encodeFullVersion(sendVersion), imagesVersion, soundsVersion)
			if err != nil {
				log.Fatalf("list characters: %v", err)
			}
			if len(names) == 0 {
				log.Fatalf("no characters available for account %s", *account)
			}
			selected := false
			if nameProvided {
				for _, n := range names {
					if n == *name {
						fmt.Println("selected character:", *name)
						selected = true
						break
					}
				}
				if !selected {
					fmt.Printf("character %s not found in account %s\n", *name, *account)
				}
			}
			if !selected {
				if len(names) == 1 {
					*name = names[0]
					fmt.Println("selected character:", *name)
				} else {
					fmt.Println("available characters:")
					for i, n := range names {
						fmt.Printf("%d) %s\n", i+1, n)
					}
					fmt.Print("select character: ")
					var choice int
					for {
						if _, err := fmt.Scanln(&choice); err != nil || choice < 1 || choice > len(names) {
							fmt.Printf("enter a number between 1 and %d: ", len(names))
							continue
						}
						break
					}
					*name = names[choice-1]
					fmt.Println("selected character:", *name)
				}
			}
		}
		playerName = *name

		answer, err := answerChallenge(*pass, challenge)
		if err != nil {
			log.Fatalf("hash: %v", err)
		}

		const kMsgLogOn = 13
		nameBytes := encodeMacRoman(*name)
		buf := make([]byte, 16+len(nameBytes)+1+len(answer))
		binary.BigEndian.PutUint16(buf[0:2], kMsgLogOn)
		binary.BigEndian.PutUint16(buf[2:4], 0)
		binary.BigEndian.PutUint32(buf[4:8], encodeFullVersion(sendVersion))
		binary.BigEndian.PutUint32(buf[8:12], imagesVersion)
		binary.BigEndian.PutUint32(buf[12:16], soundsVersion)
		copy(buf[16:], nameBytes)
		buf[16+len(nameBytes)] = 0
		copy(buf[17+len(nameBytes):], answer)
		simpleEncrypt(buf[16:])

		if err := sendTCPMessage(tcpConn, buf); err != nil {
			log.Fatalf("send login: %v", err)
		}

		resp, err := readTCPMessage(tcpConn)
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

			if err := sendPlayerInput(udpConn); err != nil {
				fmt.Printf("send player input: %v\n", err)
			}

			go sendInputLoop(ctx, udpConn)
			go udpReadLoop(ctx, udpConn)
			go tcpReadLoop(ctx, tcpConn)

			runGame(ctx)
			stop()
			tcpConn.Close()
			udpConn.Close()
		}
		break
	}
}

func extractMoviePlayerName(frames [][]byte) string {
	for _, m := range frames {
		if len(m) >= 2 && binary.BigEndian.Uint16(m[:2]) == 2 {
			data := append([]byte(nil), m[2:]...)
			if n := playerFromDrawState(data); n != "" {
				return n
			}
			simpleEncrypt(data)
			if n := playerFromDrawState(data); n != "" {
				return n
			}
		}
	}

	for _, m := range frames {
		if len(m) >= 2 && binary.BigEndian.Uint16(m[:2]) == 2 {
			data := append([]byte(nil), m[2:]...)
			if n := firstDescriptorName(data); n != "" {
				return n
			}
			simpleEncrypt(data)
			if n := firstDescriptorName(data); n != "" {
				return n
			}
		}
	}
	return ""
}

func playerFromDrawState(data []byte) string {
	if len(data) < 9 {
		return ""
	}
	p := 9
	if len(data) <= p {
		return ""
	}
	descCount := int(data[p])
	p++
	descs := make(map[uint8]struct {
		Type uint8
		Name string
	}, descCount)
	for i := 0; i < descCount && p < len(data); i++ {
		if p+4 > len(data) {
			return ""
		}
		idx := data[p]
		typ := data[p+1]
		p += 4
		if off := bytes.IndexByte(data[p:], 0); off >= 0 {
			name := string(data[p : p+off])
			p += off + 1
			if p >= len(data) {
				return ""
			}
			cnt := int(data[p])
			p++
			if p+cnt > len(data) {
				return ""
			}
			p += cnt
			descs[idx] = struct {
				Type uint8
				Name string
			}{typ, name}
		} else {
			return ""
		}
	}
	if len(data) < p+7 {
		return ""
	}
	p += 7
	if len(data) <= p {
		return ""
	}
	pictCount := int(data[p])
	p++
	if pictCount == 255 {
		if len(data) < p+2 {
			return ""
		}
		// skip pictAgain
		pictCount = int(data[p+1])
		p += 2
	}
	br := bitReader{data: data[p:]}
	for i := 0; i < pictCount; i++ {
		br.readBits(14)
		br.readBits(11)
		br.readBits(11)
	}
	p += br.bitPos / 8
	if br.bitPos%8 != 0 {
		p++
	}
	if len(data) <= p {
		return ""
	}
	mobileCount := int(data[p])
	p++
	for i := 0; i < mobileCount && p+7 <= len(data); i++ {
		idx := data[p]
		h := int16(binary.BigEndian.Uint16(data[p+2:]))
		v := int16(binary.BigEndian.Uint16(data[p+4:]))
		p += 7
		if h == 0 && v == 0 {
			if d, ok := descs[idx]; ok && d.Type == kDescPlayer {
				playerIndex = idx
				return d.Name
			}
		}
	}
	return ""
}

func firstDescriptorName(data []byte) string {
	if len(data) < 10 {
		return ""
	}
	p := 9
	if len(data) <= p {
		return ""
	}
	descCount := int(data[p])
	p++
	if descCount == 0 || p >= len(data) {
		return ""
	}
	if p+4 > len(data) {
		return ""
	}
	p += 4
	if idx := bytes.IndexByte(data[p:], 0); idx >= 0 {
		return string(data[p : p+idx])
	}
	return ""
}

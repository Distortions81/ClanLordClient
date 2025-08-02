package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
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
	clmov := flag.String("clmov", "test.clMov", "play back a .clMov file")
	name := flag.String("name", "demo", "character name")
	pass := flag.String("pass", "demo", "character password")
	clientVer := flag.Int("client-version", 1440, "client version number (kVersionNumber)")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.IntVar(&scale, "scale", 2, "screen scale factor")
	flag.BoolVar(&interp, "interp", true, "enable movement interpolation")
	flag.BoolVar(&onion, "onion", false, "cross-fade sprite animations")
	flag.BoolVar(&linear, "linear", false, "use linear filtering")
	flag.Parse()

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

	autoDemo := *name == "demo" && *pass == "demo"

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
		playerName = *name

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
	stateMu.Lock()
	if len(state.descriptors) > 0 {
		var (
			best uint8 = 0xff
			name string
		)
		for idx, d := range state.descriptors {
			if name == "" || idx < best {
				best = idx
				name = d.Name
			}
		}
		stateMu.Unlock()
		return name
	}
	stateMu.Unlock()

	for _, m := range frames {
		if len(m) >= 2 && binary.BigEndian.Uint16(m[:2]) == 2 {
			data := append([]byte(nil), m[2:]...)
			simpleEncrypt(data)
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

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
)

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
	flags := uint16(0)

	x, y := ebiten.CursorPosition()
	mouseX = int16(x/scale - fieldCenterX)
	mouseY = int16(y/scale - fieldCenterY)
	mouseDown = ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	if mouseDown {
		flags = kPIMDownField
	}
	buf := make([]byte, 20+1)
	binary.BigEndian.PutUint16(buf[0:2], kMsgPlayerInput)
	binary.BigEndian.PutUint16(buf[2:4], uint16(mouseX))
	binary.BigEndian.PutUint16(buf[4:6], uint16(mouseY))
	binary.BigEndian.PutUint16(buf[6:8], flags)
	binary.BigEndian.PutUint32(buf[8:12], uint32(ackFrame))
	binary.BigEndian.PutUint32(buf[12:16], uint32(resendFrame))
	binary.BigEndian.PutUint32(buf[16:20], commandNum)
	buf[20] = 0
	commandNum++
	dlog("player input ack=%d resend=%d cmd=%d mouse=%d,%d flags=%#x", ackFrame, resendFrame, commandNum-1, mouseX, mouseY, flags)
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
	_ = binary.BigEndian.Uint32(data[0:4])
	_ = binary.BigEndian.Uint32(data[4:8])
	_ = binary.BigEndian.Uint32(data[8:12])

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

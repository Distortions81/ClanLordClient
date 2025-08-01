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

// tcpConn is the active TCP connection to the game server.
var tcpConn net.Conn

// sendClientIdentifiers transmits the client, image and sound versions to the server.
func sendClientIdentifiers(connection net.Conn, clientVersion, imagesVersion, soundsVersion uint32) error {
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

	unameBytes := encodeMacRoman(uname)
	hnameBytes := encodeMacRoman(hname)
	bootBytes := encodeMacRoman(boot)

	data := make([]byte, 0, 8+6+len(unameBytes)+1+len(hnameBytes)+1+len(bootBytes)+1+1)
	data = append(data, make([]byte, 8)...) // magic file info placeholder
	data = append(data, make([]byte, 6)...) // ethernet address placeholder
	data = append(data, unameBytes...)
	data = append(data, 0)
	data = append(data, hnameBytes...)
	data = append(data, 0)
	data = append(data, bootBytes...)
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
	return sendTCPMessage(connection, buf)
}

// sendTCPMessage writes a length-prefixed message to the TCP connection.
func sendTCPMessage(connection net.Conn, payload []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	if _, err := connection.Write(size[:]); err != nil {
		return err
	}
	_, err := connection.Write(payload)
	tag := binary.BigEndian.Uint16(payload[:2])
	dlog("send tcp tag %d len %d", tag, len(payload))
	hexDump("send", payload)
	return err
}

// sendUDPMessage writes a length-prefixed message to the UDP connection.
func sendUDPMessage(connection net.Conn, payload []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(payload)))
	buf := append(size[:], payload...)
	_, err := connection.Write(buf)
	tag := binary.BigEndian.Uint16(payload[:2])
	dlog("send udp tag %d len %d", tag, len(payload))
	hexDump("send", payload)
	return err
}

// readUDPMessage reads a single length-prefixed message from the UDP connection.
func readUDPMessage(connection net.Conn) ([]byte, error) {
	buf := make([]byte, 65535)
	n, err := connection.Read(buf)
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

// sendPlayerInput sends the current mouse state to the server via UDP.
func sendPlayerInput(connection net.Conn) error {
	const kMsgPlayerInput = 3
	flags := uint16(0)

	x, y := ebiten.CursorPosition()
	mouseX = int16(x/scale - fieldCenterX)
	mouseY = int16(y/scale - fieldCenterY)
	mouseDown = ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)

	if mouseDown {
		flags = kPIMDownField
	}
	packet := make([]byte, 20+1)
	binary.BigEndian.PutUint16(packet[0:2], kMsgPlayerInput)
	binary.BigEndian.PutUint16(packet[2:4], uint16(mouseX))
	binary.BigEndian.PutUint16(packet[4:6], uint16(mouseY))
	binary.BigEndian.PutUint16(packet[6:8], flags)
	binary.BigEndian.PutUint32(packet[8:12], uint32(ackFrame))
	binary.BigEndian.PutUint32(packet[12:16], uint32(resendFrame))
	binary.BigEndian.PutUint32(packet[16:20], commandNum)
	packet[20] = 0
	commandNum++
	dlog("player input ack=%d resend=%d cmd=%d mouse=%d,%d flags=%#x", ackFrame, resendFrame, commandNum-1, mouseX, mouseY, flags)
	return sendUDPMessage(connection, packet)
}

// readTCPMessage reads a single length-prefixed message from the TCP connection.
func readTCPMessage(connection net.Conn) ([]byte, error) {
	var sizeBuf [2]byte
	if _, err := io.ReadFull(connection, sizeBuf[:]); err != nil {
		return nil, err
	}
	sz := binary.BigEndian.Uint16(sizeBuf[:])
	buf := make([]byte, sz)
	if _, err := io.ReadFull(connection, buf); err != nil {
		return nil, err
	}
	tag := binary.BigEndian.Uint16(buf[:2])
	dlog("recv tcp tag %d len %d", tag, len(buf))
	hexDump("recv", buf)
	return buf, nil
}

// requestCharList fetches the list of characters for an account from the server.
// When the user supplies an account on the command line, the client uses this
// to prompt for which character to log in with.
func requestCharList(connection net.Conn, account, accountPass string, challenge []byte, clientVersion, imagesVersion, soundsVersion uint32) ([]string, error) {
	if err := sendCharListRequest(connection, account, accountPass, challenge, clientVersion, imagesVersion, soundsVersion); err != nil {
		return nil, err
	}
	resp, err := readTCPMessage(connection)
	if err != nil {
		return nil, err
	}
	return parseCharListResponse(resp)
}

// sendCharListRequest builds and sends a character list request to the server
// for the specified account.
func sendCharListRequest(connection net.Conn, account, accountPass string, challenge []byte, clientVersion, imagesVersion, soundsVersion uint32) error {
	answer, err := answerChallenge(accountPass, challenge)
	if err != nil {
		return err
	}
	const kMsgCharList = 14
	accountBytes := encodeMacRoman(account)
	packet := make([]byte, 16+len(accountBytes)+1+len(answer))
	binary.BigEndian.PutUint16(packet[0:2], kMsgCharList)
	binary.BigEndian.PutUint16(packet[2:4], 0)
	binary.BigEndian.PutUint32(packet[4:8], clientVersion)
	binary.BigEndian.PutUint32(packet[8:12], imagesVersion)
	binary.BigEndian.PutUint32(packet[12:16], soundsVersion)
	copy(packet[16:], accountBytes)
	packet[16+len(accountBytes)] = 0
	copy(packet[17+len(accountBytes):], answer)
	simpleEncrypt(packet[16:])
	dlog("request character list for %s", account)
	return sendTCPMessage(connection, packet)
}

// parseCharListResponse decrypts and parses the character list response,
// returning the available character names.
func parseCharListResponse(resp []byte) ([]string, error) {
	const kMsgCharList = 14
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
		if i <= 0 {
			break
		}
		names = append(names, string(namesData[:i]))
		namesData = namesData[i+1:]
	}
	dlog("server returned %d characters", len(names))
	return names, nil
}

// sendCommandText sends a text command to the server over TCP.
func sendCommandText(connection net.Conn, txt string) error {
	const kMsgCommandText = 6
	txtBytes := encodeMacRoman(txt)
	data := append(txtBytes, 0)
	packet := make([]byte, 16+len(data))
	binary.BigEndian.PutUint16(packet[0:2], kMsgCommandText)
	binary.BigEndian.PutUint16(packet[2:4], 0)
	copy(packet[16:], data)
	simpleEncrypt(packet[16:])
	fmt.Printf("send command: %s\n", txt)
	return sendTCPMessage(connection, packet)
}

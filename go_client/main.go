package main

import (
	"bytes"
	"crypto/aes"
	"crypto/md5"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
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

func sendMessage(conn net.Conn, msg []byte) error {
	var size [2]byte
	binary.BigEndian.PutUint16(size[:], uint16(len(msg)))
	if _, err := conn.Write(size[:]); err != nil {
		return err
	}
	_, err := conn.Write(msg)
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
	return buf, nil
}

func requestCharList(conn net.Conn, account, password string, challenge []byte) ([]string, error) {
	answer, err := answerChallenge(password, challenge)
	if err != nil {
		return nil, err
	}

	const kMsgCharList = 14
	const clientVersion = 346368
	buf := make([]byte, 16+len(account)+1+len(answer))
	binary.BigEndian.PutUint16(buf[0:2], kMsgCharList)
	binary.BigEndian.PutUint16(buf[2:4], 0)
	binary.BigEndian.PutUint32(buf[4:8], clientVersion)
	binary.BigEndian.PutUint32(buf[8:12], 0)
	binary.BigEndian.PutUint32(buf[12:16], 0)
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
	result := binary.BigEndian.Uint16(resp[2:4])
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
	flag.Parse()

	tcpConn, err := net.Dial("tcp", *host)
	if err != nil {
		log.Fatalf("tcp connect: %v", err)
	}
	defer tcpConn.Close()

	udpConn, err := net.Dial("udp", *host)
	if err != nil {
		log.Fatalf("udp connect: %v", err)
	}
	defer udpConn.Close()

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

	// wait for challenge message
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
	challenge := msg[8 : 8+16] // skip header fields

	if *listDemo {
		names, err := requestCharList(tcpConn, "demo", "demo", challenge)
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
	const clientVersion = 346368
	buf := make([]byte, 16+len(*name)+1+len(answer))
	binary.BigEndian.PutUint16(buf[0:2], kMsgLogOn)
	binary.BigEndian.PutUint16(buf[2:4], 0)
	binary.BigEndian.PutUint32(buf[4:8], clientVersion)
	binary.BigEndian.PutUint32(buf[8:12], 0)
	binary.BigEndian.PutUint32(buf[12:16], 0)
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
	result := binary.BigEndian.Uint16(resp[2:4])
	fmt.Printf("login result: %d\n", result)
}

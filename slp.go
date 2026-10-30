package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// pingResult is what the Minecraft "Server List Ping" reports, the same
// data the multiplayer screen shows. It is the authoritative player count;
// parsing the console only helps with names.
type pingResult struct {
	Online int
	Max    int
	Sample []string // up to ~12 names; empty if the server hides them
}

// pingServer runs the status handshake against host:port.
func pingServer(host string, port int, timeout time.Duration) (*pingResult, error) {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, fmt.Sprint(port)), timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(timeout))

	// Handshake: protocol version -1 means "just asking for status".
	var hs bytes.Buffer
	writeVarInt(&hs, 0x00)
	writeVarInt(&hs, -1)
	writeVarInt(&hs, int32(len(host)))
	hs.WriteString(host)
	binary.Write(&hs, binary.BigEndian, uint16(port))
	writeVarInt(&hs, 1) // next state: status
	if err := writePacket(conn, hs.Bytes()); err != nil {
		return nil, err
	}
	if err := writePacket(conn, []byte{0x00}); err != nil { // status request
		return nil, err
	}

	r := bufio.NewReader(conn)
	length, err := readVarInt(r)
	if err != nil {
		return nil, err
	}
	if length <= 0 || length > 1<<21 {
		return nil, errors.New("bad status packet length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	br := bytes.NewReader(body)
	if id, err := readVarInt(br); err != nil || id != 0x00 {
		return nil, errors.New("unexpected status packet")
	}
	strLen, err := readVarInt(br)
	if err != nil || strLen < 0 || int(strLen) > br.Len() {
		return nil, errors.New("bad status string")
	}
	jsonBytes := make([]byte, strLen)
	io.ReadFull(br, jsonBytes)

	var status struct {
		Players struct {
			Max    int `json:"max"`
			Online int `json:"online"`
			Sample []struct {
				Name string `json:"name"`
			} `json:"sample"`
		} `json:"players"`
	}
	if err := json.Unmarshal(jsonBytes, &status); err != nil {
		return nil, err
	}
	res := &pingResult{Online: status.Players.Online, Max: status.Players.Max}
	for _, s := range status.Players.Sample {
		// Servers that hide players send a placeholder like "Anonymous Player".
		if reName.MatchString(s.Name) {
			res.Sample = append(res.Sample, s.Name)
		}
	}
	return res, nil
}

func writePacket(w io.Writer, payload []byte) error {
	var b bytes.Buffer
	writeVarInt(&b, int32(len(payload)))
	b.Write(payload)
	_, err := w.Write(b.Bytes())
	return err
}

func writeVarInt(b *bytes.Buffer, v int32) {
	u := uint32(v)
	for {
		if u&^0x7f == 0 {
			b.WriteByte(byte(u))
			return
		}
		b.WriteByte(byte(u&0x7f | 0x80))
		u >>= 7
	}
}

func readVarInt(r io.ByteReader) (int32, error) {
	var result uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		result |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), nil
		}
	}
	return 0, errors.New("varint too long")
}

package ws

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	opcodeText  = 0x1
	opcodeClose = 0x8
)

type Frame struct {
	FIN     bool
	Opcode  byte
	Masked  bool
	Payload []byte
}

func ReadFrame(r *bufio.Reader) (Frame, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}

	f := Frame{
		FIN:    header[0]&0x80 != 0,
		Opcode: header[0] & 0x0f,
		Masked: header[1]&0x80 != 0,
	}

	payloadLen := uint64(header[1] & 0x7f)
	switch payloadLen {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, err
		}
		payloadLen = uint64(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, err
		}
		payloadLen = binary.BigEndian.Uint64(ext[:])
	}

	var maskKey [4]byte
	if f.Masked {
		if _, err := io.ReadFull(r, maskKey[:]); err != nil {
			return Frame{}, err
		}
	}

	if payloadLen > 1<<20 {
		return Frame{}, fmt.Errorf("payload too large: %d", payloadLen)
	}
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Frame{}, err
	}

	if f.Masked {
		applyMask(payload, maskKey)
	}
	f.Payload = payload
	return f, nil
}

func WriteFrame(w io.Writer, payload []byte, opcode byte, masked bool) error {
	if len(payload) > 125 {
		return errors.New("only small payloads are supported in the spike writer")
	}

	first := byte(0x80) | opcode
	second := byte(len(payload))
	if masked {
		second |= 0x80
	}

	if _, err := w.Write([]byte{first, second}); err != nil {
		return err
	}

	body := append([]byte(nil), payload...)
	if masked {
		var maskKey [4]byte
		if _, err := rand.Read(maskKey[:]); err != nil {
			return err
		}
		if _, err := w.Write(maskKey[:]); err != nil {
			return err
		}
		applyMask(body, maskKey)
	}

	_, err := w.Write(body)
	return err
}

func applyMask(payload []byte, key [4]byte) {
	for i := range payload {
		payload[i] ^= key[i%4]
	}
}

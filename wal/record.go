package wal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
)

type Record struct {
	Type    uint8
	Payload []byte
}

func (r Record) Encode() ([]byte, error) {
	if uint64(len(r.Payload)) > uint64(MaxPayloadSize) {
		return nil, fmt.Errorf("wal: payload too large: %d bytes (max %d)", len(r.Payload), MaxPayloadSize)
	}
	bodyLen := uint32(1 + len(r.Payload))
	out := make([]byte, RecordHeaderSize+len(r.Payload))

	// Layout: [length:4][crc:4][type:1][payload]. The body is everything from the
	// type byte onward; the CRC covers exactly that body.
	binary.LittleEndian.PutUint32(out[0:4], bodyLen)
	body := out[LengthFieldSize+CRCFieldSize:]
	body[0] = r.Type
	copy(body[1:], r.Payload)
	binary.LittleEndian.PutUint32(out[LengthFieldSize:LengthFieldSize+CRCFieldSize], crc32.ChecksumIEEE(body))

	return out, nil
}

func DecodeRecord(rd io.Reader) (Record, error) {
	// The fixed-width prefix is length + crc; the type byte lives in the body.
	var prefix [LengthFieldSize + CRCFieldSize]byte
	// io.ReadFull: returns io.EOF only when zero bytes were read; a partial read becomes io.ErrUnexpectedEOF.
	if _, err := io.ReadFull(rd, prefix[:]); err != nil {
		return Record{}, err
	}
	bodyLen := binary.LittleEndian.Uint32(prefix[0:LengthFieldSize])
	wantCRC := binary.LittleEndian.Uint32(prefix[LengthFieldSize : LengthFieldSize+CRCFieldSize])

	if bodyLen < 1 {
		return Record{}, fmt.Errorf("%w: zero-length body", ErrCorruptRecord)
	}
	if bodyLen-1 > MaxPayloadSize {
		return Record{}, fmt.Errorf("%w: body length %d exceeds max %d", ErrCorruptRecord, bodyLen, MaxPayloadSize)
	}

	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(rd, body); err != nil {
		if err == io.EOF {
			return Record{}, io.ErrUnexpectedEOF
		}
		return Record{}, err
	}

	if got := crc32.ChecksumIEEE(body); got != wantCRC {
		return Record{}, fmt.Errorf("%w: crc mismatch (stored 0x%08x, computed 0x%08x)", ErrCorruptRecord, wantCRC, got)
	}

	rec := Record{Type: body[0]}
	if payloadLen := bodyLen - 1; payloadLen > 0 {
		rec.Payload = make([]byte, payloadLen)
		copy(rec.Payload, body[1:])
	}
	return rec, nil
}

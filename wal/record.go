package wal

import (
	"encoding/binary"
	"fmt"
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
	binary.LittleEndian.PutUint32(out[0:4], bodyLen)
	out[4] = r.Type
	copy(out[5:], r.Payload)
	return out, nil
}

func DecodeRecord(rd io.Reader) (Record, error) {
	var header [RecordHeaderSize]byte
	// io.ReadFull: returns io.EOF only when zero bytes were read; a partial read becomes io.ErrUnexpectedEOF.
	if _, err := io.ReadFull(rd, header[:]); err != nil {
		return Record{}, err
	}
	bodyLen := binary.LittleEndian.Uint32(header[0:4])
	if bodyLen < 1 {
		return Record{}, fmt.Errorf("wal: invalid record length: %d", bodyLen)
	}
	rec := Record{Type: header[4]}
	payloadLen := bodyLen - 1
	if payloadLen > 0 {
		rec.Payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(rd, rec.Payload); err != nil {
			if err == io.EOF {
				return Record{}, io.ErrUnexpectedEOF
			}
			return Record{}, err
		}
	}
	return rec, nil
}

package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

func TestRecordRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
	}{
		{"empty payload", Record{Type: RecordTypeRaw, Payload: nil}},
		{"single byte", Record{Type: RecordTypeRaw, Payload: []byte{0x42}}},
		{"short string", Record{Type: RecordTypeRaw, Payload: []byte("hello, durable")}},
		{"contains zero bytes", Record{Type: RecordTypeRaw, Payload: []byte{0x00, 0x01, 0x00, 0x02, 0x00}}},
		{"1 KB payload", Record{Type: RecordTypeRaw, Payload: bytes.Repeat([]byte{0xAB}, 1024)}},
		{"1 MB payload", Record{Type: RecordTypeRaw, Payload: bytes.Repeat([]byte{0xCD}, 1<<20)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := tc.rec.Encode()
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			wantSize := RecordHeaderSize + len(tc.rec.Payload)
			if len(encoded) != wantSize {
				t.Fatalf("encoded length = %d, want %d", len(encoded), wantSize)
			}

			gotLen := binary.LittleEndian.Uint32(encoded[0:4])
			wantLen := uint32(1 + len(tc.rec.Payload))
			if gotLen != wantLen {
				t.Fatalf("length field = %d, want %d", gotLen, wantLen)
			}

			if encoded[4] != tc.rec.Type {
				t.Fatalf("type byte = 0x%02x, want 0x%02x", encoded[4], tc.rec.Type)
			}

			decoded, err := DecodeRecord(bytes.NewReader(encoded))
			if err != nil {
				t.Fatalf("DecodeRecord: %v", err)
			}
			if decoded.Type != tc.rec.Type {
				t.Fatalf("decoded type = 0x%02x, want 0x%02x", decoded.Type, tc.rec.Type)
			}
			if !bytes.Equal(decoded.Payload, tc.rec.Payload) {
				t.Fatalf("decoded payload != original (len got=%d want=%d)", len(decoded.Payload), len(tc.rec.Payload))
			}
		})
	}
}

func TestDecodeEmptyReaderReturnsEOF(t *testing.T) {
	_, err := DecodeRecord(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("DecodeRecord on empty reader: err = %v, want io.EOF", err)
	}
}

func TestDecodeTruncatedHeaderReturnsUnexpectedEOF(t *testing.T) {
	_, err := DecodeRecord(bytes.NewReader([]byte{0x01, 0x00}))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeRecord on truncated header: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestDecodeTruncatedPayloadReturnsUnexpectedEOF(t *testing.T) {
	buf := make([]byte, 0, 8)
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], 10) // body = 1 type + 9 payload
	buf = append(buf, header[:]...)
	buf = append(buf, RecordTypeRaw)
	buf = append(buf, 0x01, 0x02, 0x03)
	_, err := DecodeRecord(bytes.NewReader(buf))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeRecord on truncated payload: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestDecodeZeroLengthIsError(t *testing.T) {
	var header [4]byte
	binary.LittleEndian.PutUint32(header[:], 0)
	_, err := DecodeRecord(bytes.NewReader(header[:]))
	if err == nil {
		t.Fatalf("DecodeRecord on zero-length record: err = nil, want error")
	}
}

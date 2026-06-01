package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"testing"
)

// frame builds a valid record frame for tests that then corrupt or truncate it.
func frame(typ uint8, payload []byte) []byte {
	rec := Record{Type: typ, Payload: payload}
	encoded, err := rec.Encode()
	if err != nil {
		panic(err)
	}
	return encoded
}

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

			gotLen := binary.LittleEndian.Uint32(encoded[0:LengthFieldSize])
			wantLen := uint32(1 + len(tc.rec.Payload))
			if gotLen != wantLen {
				t.Fatalf("length field = %d, want %d", gotLen, wantLen)
			}

			body := encoded[LengthFieldSize+CRCFieldSize:]
			gotCRC := binary.LittleEndian.Uint32(encoded[LengthFieldSize : LengthFieldSize+CRCFieldSize])
			if wantCRC := crc32.ChecksumIEEE(body); gotCRC != wantCRC {
				t.Fatalf("crc field = 0x%08x, want 0x%08x", gotCRC, wantCRC)
			}

			if body[0] != tc.rec.Type {
				t.Fatalf("type byte = 0x%02x, want 0x%02x", body[0], tc.rec.Type)
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
	// Cut a 9-byte payload down to 3 surviving bytes.
	full := frame(RecordTypeRaw, []byte{0, 1, 2, 3, 4, 5, 6, 7, 8})
	truncated := full[:RecordHeaderSize+3]
	_, err := DecodeRecord(bytes.NewReader(truncated))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("DecodeRecord on truncated payload: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestDecodeZeroLengthIsCorrupt(t *testing.T) {
	// A real body always holds at least the type byte, so length 0 is impossible.
	var prefix [LengthFieldSize + CRCFieldSize]byte
	binary.LittleEndian.PutUint32(prefix[0:LengthFieldSize], 0)
	_, err := DecodeRecord(bytes.NewReader(prefix[:]))
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("DecodeRecord on zero-length record: err = %v, want ErrCorruptRecord", err)
	}
}

func TestDecodeDetectsCorruptedPayload(t *testing.T) {
	buf := frame(RecordTypeRaw, []byte("durable payload"))
	buf[RecordHeaderSize+2] ^= 0xFF // flip a byte in the payload; CRC no longer matches
	_, err := DecodeRecord(bytes.NewReader(buf))
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("DecodeRecord on corrupted payload: err = %v, want ErrCorruptRecord", err)
	}
}

func TestDecodeDetectsCorruptedType(t *testing.T) {
	buf := frame(RecordTypeRaw, []byte("durable payload"))
	buf[LengthFieldSize+CRCFieldSize] ^= 0xFF // flip the type byte; it is covered by the CRC
	_, err := DecodeRecord(bytes.NewReader(buf))
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("DecodeRecord on corrupted type byte: err = %v, want ErrCorruptRecord", err)
	}
}

func TestDecodeDetectsCorruptedCRCField(t *testing.T) {
	buf := frame(RecordTypeRaw, []byte("durable payload"))
	buf[LengthFieldSize] ^= 0xFF // flip a byte of the stored CRC itself
	_, err := DecodeRecord(bytes.NewReader(buf))
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("DecodeRecord on corrupted crc field: err = %v, want ErrCorruptRecord", err)
	}
}

func TestDecodeOverlongLengthIsCorrupt(t *testing.T) {
	// A length past MaxPayloadSize must be rejected before any allocation.
	var prefix [LengthFieldSize + CRCFieldSize]byte
	binary.LittleEndian.PutUint32(prefix[0:LengthFieldSize], 0xFFFFFFFF)
	_, err := DecodeRecord(bytes.NewReader(prefix[:]))
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("DecodeRecord on overlong length: err = %v, want ErrCorruptRecord", err)
	}
}

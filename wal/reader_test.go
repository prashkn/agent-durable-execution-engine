package wal

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"testing"
)

func TestReaderRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rt.log")

	records := []Record{
		{Type: RecordTypeRaw, Payload: []byte("alpha")},
		{Type: RecordTypeRaw, Payload: []byte{}},
		{Type: RecordTypeRaw, Payload: []byte("gamma is longer than the others")},
		{Type: RecordTypeRaw, Payload: bytes.Repeat([]byte{0xAA, 0xBB}, 4096)},
	}

	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i, rec := range records {
		if err := w.Append(rec); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	for i, want := range records {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next #%d: %v", i, err)
		}
		if got.Type != want.Type {
			t.Fatalf("record %d: type = 0x%02x, want 0x%02x", i, got.Type, want.Type)
		}
		if !bytes.Equal(got.Payload, want.Payload) {
			t.Fatalf("record %d: payload mismatch (len got=%d want=%d)", i, len(got.Payload), len(want.Payload))
		}
	}

	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next after drain: err = %v, want io.EOF", err)
	}
}

func TestReaderEmptyLogReturnsEOFImmediately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	r, err := NewReader(path)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	defer r.Close()

	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Fatalf("Next on empty log: err = %v, want io.EOF", err)
	}
}

func TestReaderMissingFileErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := NewReader(filepath.Join(dir, "nope.log"))
	if err == nil {
		t.Fatalf("NewReader on missing file: err = nil, want error")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("NewReader on missing file: err = %v, want fs.ErrNotExist", err)
	}
}

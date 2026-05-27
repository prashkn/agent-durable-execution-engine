package wal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWriterWritesFramedBytes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}

	records := []Record{
		{Type: RecordTypeRaw, Payload: []byte("first")},
		{Type: RecordTypeRaw, Payload: []byte("second record")},
		{Type: RecordTypeRaw, Payload: bytes.Repeat([]byte{0xFF}, 256)},
	}

	var want bytes.Buffer
	for _, r := range records {
		if err := w.Append(r); err != nil {
			t.Fatalf("Append: %v", err)
		}
		enc, err := r.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		want.Write(enc)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Fatalf("file bytes != expected (len got=%d want=%d)", len(got), want.Len())
	}
}

func TestWriterCreatesEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fresh.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("precondition: file should not exist, got %v", err)
	}
	w, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Size() != 0 {
		t.Fatalf("new file size = %d, want 0", info.Size())
	}
}

func TestWriterAppendsAcrossOpens(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "appendy.log")

	w1, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter #1: %v", err)
	}
	rec1 := Record{Type: RecordTypeRaw, Payload: []byte("one")}
	if err := w1.Append(rec1); err != nil {
		t.Fatalf("Append #1: %v", err)
	}
	if err := w1.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}

	w2, err := NewWriter(path)
	if err != nil {
		t.Fatalf("NewWriter #2: %v", err)
	}
	rec2 := Record{Type: RecordTypeRaw, Payload: []byte("two")}
	if err := w2.Append(rec2); err != nil {
		t.Fatalf("Append #2: %v", err)
	}
	if err := w2.Close(); err != nil {
		t.Fatalf("Close #2: %v", err)
	}

	enc1, _ := rec1.Encode()
	enc2, _ := rec2.Encode()
	want := append(enc1, enc2...)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("file bytes after two opens != concat (len got=%d want=%d)", len(got), len(want))
	}
}

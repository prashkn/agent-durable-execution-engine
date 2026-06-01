package wal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerEndToEnd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mgr.log")

	const n = 100
	records := make([]Record, n)
	for i := range records {
		records[i] = Record{
			Type:    RecordTypeRaw,
			Payload: []byte(fmt.Sprintf("record-%03d-payload", i)),
		}
	}

	m1, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager #1: %v", err)
	}
	for i, rec := range records {
		if err := m1.Append(rec); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if err := m1.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}

	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager #2: %v", err)
	}
	defer m2.Close()

	var got []Record
	err = m2.Replay(func(r Record) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(got) != n {
		t.Fatalf("got %d records, want %d", len(got), n)
	}
	for i := range records {
		if got[i].Type != records[i].Type {
			t.Fatalf("record %d: type = 0x%02x, want 0x%02x", i, got[i].Type, records[i].Type)
		}
		if !bytes.Equal(got[i].Payload, records[i].Payload) {
			t.Fatalf("record %d: payload mismatch (got=%q want=%q)", i, got[i].Payload, records[i].Payload)
		}
	}
}

func TestManagerReplayStopsAtCorruptRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.log")

	// Fixed-width payloads keep on-disk offsets deterministic.
	const n = 8
	const payloadLen = 6
	recordSize := RecordHeaderSize + payloadLen

	m1, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager #1: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := m1.Append(Record{Type: RecordTypeRaw, Payload: []byte(fmt.Sprintf("rec-%02d", i))}); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if err := m1.Close(); err != nil {
		t.Fatalf("Close #1: %v", err)
	}

	// Open on the clean log first (recovery is a no-op), then corrupt a record so this
	// hits Replay's own guard instead of recovery's truncation. Recovery's path is
	// covered by TestRecoverInteriorCorruption.
	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager #2: %v", err)
	}
	defer m2.Close()

	// Corrupt a byte inside record 3's payload.
	const badIdx = 3
	corruptOffset := badIdx*recordSize + RecordHeaderSize
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data[corruptOffset] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	var got []Record
	err = m2.Replay(func(r Record) error {
		got = append(got, r)
		return nil
	})
	if !errors.Is(err, ErrCorruptRecord) {
		t.Fatalf("Replay: err = %v, want ErrCorruptRecord", err)
	}
	if len(got) != badIdx {
		t.Fatalf("delivered %d records before corruption, want %d", len(got), badIdx)
	}
	for i := range got {
		want := []byte(fmt.Sprintf("rec-%02d", i))
		if !bytes.Equal(got[i].Payload, want) {
			t.Fatalf("record %d: payload = %q, want %q", i, got[i].Payload, want)
		}
	}
}

func TestManagerReplayEmptyLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	called := false
	err = m.Replay(func(r Record) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if called {
		t.Fatalf("Replay called fn on empty log, expected no callbacks")
	}
}

func TestManagerReplayPropagatesFnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stop.log")

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	for i := 0; i < 5; i++ {
		if err := m.Append(Record{Type: RecordTypeRaw, Payload: []byte{byte(i)}}); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}

	stop := errors.New("stop")
	count := 0
	err = m.Replay(func(r Record) error {
		count++
		if count == 3 {
			return stop
		}
		return nil
	})
	if !errors.Is(err, stop) {
		t.Fatalf("Replay: err = %v, want %v", err, stop)
	}
	if count != 3 {
		t.Fatalf("Replay called fn %d times, want 3", count)
	}
}

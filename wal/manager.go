package wal

import (
	"errors"
	"fmt"
	"io"
)

type Manager struct {
	path string
	w    *Writer
}

func NewManager(path string) (*Manager, error) {
	w, err := NewWriter(path)
	if err != nil {
		return nil, fmt.Errorf("wal: new manager at %q: %w", path, err)
	}
	return &Manager{path: path, w: w}, nil
}

func (m *Manager) Append(r Record) error {
	return m.w.Append(r)
}

func (m *Manager) Replay(fn func(Record) error) error {
	r, err := NewReader(m.path)
	if err != nil {
		return fmt.Errorf("wal: open reader for replay: %w", err)
	}
	defer r.Close()

	for {
		rec, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		// Corruption (bad CRC or malformed framing) halts replay at the first bad
		// record: every good record before it has already been delivered to fn, but
		// nothing past it is trusted. Surfacing the error keeps the "no silent
		// corruption" invariant; reclaiming the log by truncation is L3's job.
		if errors.Is(err, ErrCorruptRecord) {
			return fmt.Errorf("wal: replay stopped at corrupt record: %w", err)
		}
		if err != nil {
			return fmt.Errorf("wal: read during replay: %w", err)
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
}

func (m *Manager) Close() error {
	return m.w.Close()
}

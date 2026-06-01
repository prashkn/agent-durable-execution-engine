package wal

import (
	"errors"
	"fmt"
	"io"
)

type Manager struct {
	path     string
	w        *Writer
	recovery RecoveryResult
}

func NewManager(path string) (*Manager, error) {
	// Recover before opening the writer so its append handle lands at the clean EOF.
	// A corrupt log is repaired, not rejected — failing here would make it unopenable.
	recovery, err := recoverLog(path)
	if err != nil {
		return nil, fmt.Errorf("wal: recover %q: %w", path, err)
	}

	w, err := NewWriter(path)
	if err != nil {
		return nil, fmt.Errorf("wal: new manager at %q: %w", path, err)
	}
	return &Manager{path: path, w: w, recovery: recovery}, nil
}

// Recovery reports what the startup recovery pass did, so callers can surface a
// ReasonCorrupt truncation.
func (m *Manager) Recovery() RecoveryResult {
	return m.recovery
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
		// Halt at the first corrupt record: records before it are already delivered,
		// nothing past it is trusted. (Recovery normally truncates this away first.)
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

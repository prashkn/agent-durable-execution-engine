package wal

import (
	"fmt"
	"os"
)

type Writer struct {
	f    *os.File
	path string
}

func NewWriter(path string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("wal: open %q for append: %w", path, err)
	}
	return &Writer{f: f, path: path}, nil
}

func (w *Writer) Append(r Record) error {
	buf, err := r.Encode()
	if err != nil {
		return fmt.Errorf("wal: encode record: %w", err)
	}
	if _, err := w.f.Write(buf); err != nil {
		return fmt.Errorf("wal: write to %q: %w", w.path, err)
	}
	return nil
}

func (w *Writer) Close() error {
	return w.f.Close()
}

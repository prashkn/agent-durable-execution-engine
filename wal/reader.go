package wal

import (
	"bufio"
	"fmt"
	"os"
)

type Reader struct {
	f  *os.File
	br *bufio.Reader
}

func NewReader(path string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("wal: open %q for read: %w", path, err)
	}
	return &Reader{f: f, br: bufio.NewReader(f)}, nil
}

func (r *Reader) Next() (Record, error) {
	return DecodeRecord(r.br)
}

func (r *Reader) Close() error {
	return r.f.Close()
}

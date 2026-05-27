package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/prashkn/durable/wal"
)

const logPath = "durable.log"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: durable <input string>")
		os.Exit(1)
	}

	input := strings.Join(os.Args[1:], " ")
	words := strings.Fields(input)

	m, err := wal.NewManager(logPath)
	if err != nil {
		fatal("open wal", err)
	}
	for _, word := range words {
		rec := wal.Record{Type: wal.RecordTypeRaw, Payload: []byte(word)}
		if err := m.Append(rec); err != nil {
			fatal("append", err)
		}
	}
	if err := m.Close(); err != nil {
		fatal("close", err)
	}

	fmt.Printf("appended %d records to %s\n\nreplay:\n", len(words), logPath)

	m2, err := wal.NewManager(logPath)
	if err != nil {
		fatal("reopen wal", err)
	}
	defer m2.Close()

	i := 0
	err = m2.Replay(func(r wal.Record) error {
		fmt.Printf("  [%d] type=0x%02x payload=%q\n", i, r.Type, string(r.Payload))
		i++
		return nil
	})
	if err != nil {
		fatal("replay", err)
	}
}

func fatal(ctx string, err error) {
	fmt.Fprintf(os.Stderr, "durable: %s: %v\n", ctx, err)
	os.Exit(1)
}

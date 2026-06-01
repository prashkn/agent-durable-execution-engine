package wal

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
)

// RecoveryResult is what the startup recovery pass found and did, exposed via
// Manager.Recovery.
type RecoveryResult struct {
	LastGoodOffset int64 // file size after recovery (end of the last valid record)
	TruncatedBytes int64 // bytes discarded from the tail; zero on a clean log
	Reason         RecoveryReason
}

func frameSize(r Record) int64 {
	return int64(RecordHeaderSize) + int64(len(r.Payload))
}

// recoverLog scans the log and, if its tail is unusable, truncates back to the last
// valid record so the writer can resume at a clean boundary. It reuses DecodeRecord so
// recovery can't drift from the writer's framing, and only advances the good-prefix
// length on a full decode, so a partial read never miscounts. A missing file is a no-op;
// a real I/O error aborts without truncating, so a transient read never loses data.
func recoverLog(path string) (RecoveryResult, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RecoveryResult{Reason: ReasonClean}, nil
		}
		return RecoveryResult{}, fmt.Errorf("wal: open %q for recovery: %w", path, err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return RecoveryResult{}, fmt.Errorf("wal: stat %q for recovery: %w", path, err)
	}
	size := info.Size()

	var goodLen int64
	reason := ReasonClean
	br := bufio.NewReader(f)

	for {
		rec, decErr := DecodeRecord(br)
		if decErr == nil {
			goodLen += frameSize(rec)
			continue
		}
		switch {
		case errors.Is(decErr, io.EOF):
			reason = ReasonClean
		case errors.Is(decErr, io.ErrUnexpectedEOF):
			reason = ReasonTornTail
		case errors.Is(decErr, ErrCorruptRecord):
			reason = ReasonCorrupt
		default:
			// Don't truncate on a transient read error — that would lose good data.
			return RecoveryResult{}, fmt.Errorf("wal: read during recovery of %q: %w", path, decErr)
		}
		break
	}

	if goodLen == size {
		return RecoveryResult{LastGoodOffset: goodLen, Reason: ReasonClean}, nil
	}

	// fsync the new size so a second crash can't resurrect the discarded tail.
	if err := f.Truncate(goodLen); err != nil {
		return RecoveryResult{}, fmt.Errorf("wal: truncate %q to %d during recovery: %w", path, goodLen, err)
	}
	if err := f.Sync(); err != nil {
		return RecoveryResult{}, fmt.Errorf("wal: sync %q after recovery truncation: %w", path, err)
	}

	return RecoveryResult{
		LastGoodOffset: goodLen,
		TruncatedBytes: size - goodLen,
		Reason:         reason,
	}, nil
}

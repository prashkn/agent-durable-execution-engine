package wal

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixedRecord has a constant 8-byte payload, so frame offsets are predictable.
func fixedRecord(i int) Record {
	return Record{Type: RecordTypeRaw, Payload: []byte(fmt.Sprintf("rec-%04d", i))}
}

// writeRecords appends n fixed records to a fresh log.
func writeRecords(t *testing.T, path string, n int) {
	t.Helper()
	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	for i := 0; i < n; i++ {
		if err := m.Append(fixedRecord(i)); err != nil {
			t.Fatalf("Append #%d: %v", i, err)
		}
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// appendRaw writes raw bytes to the tail, modeling a partial frame left by a crash.
func appendRaw(t *testing.T, path string, b []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for raw append: %v", err)
	}
	if _, err := f.Write(b); err != nil {
		t.Fatalf("raw append: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close after raw append: %v", err)
	}
}

// replayCollect drains the log through Replay and returns the records delivered.
func replayCollect(t *testing.T, m *Manager) []Record {
	t.Helper()
	var got []Record
	if err := m.Replay(func(r Record) error {
		got = append(got, r)
		return nil
	}); err != nil {
		t.Fatalf("Replay: %v", err)
	}
	return got
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %q: %v", path, err)
	}
	return info.Size()
}

// TestRecoverTornTail writes n complete frames plus the first k bytes of the next, then
// checks recovery truncates back to n frames and the log stays appendable.
func TestRecoverTornTail(t *testing.T) {
	const n = 5
	fs := frameSize(fixedRecord(0))
	full, err := fixedRecord(n).Encode()
	if err != nil {
		t.Fatalf("Encode partial frame: %v", err)
	}

	cases := []struct {
		name string
		k    int
	}{
		{"one-byte", 1},
		{"mid-header", LengthFieldSize}, // inside the length field
		{"full-prefix-no-body", LengthFieldSize + CRCFieldSize},      // header read OK, body empty
		{"prefix-plus-one-body", LengthFieldSize + CRCFieldSize + 1}, // body partially present
		{"frame-minus-one", len(full) - 1},                           // one byte short of a full frame
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "torn.log")
			writeRecords(t, path, n)
			appendRaw(t, path, full[:tc.k])

			goodLen := int64(n) * fs

			m, err := NewManager(path)
			if err != nil {
				t.Fatalf("NewManager: %v", err)
			}
			res := m.Recovery()
			if res.Reason != ReasonTornTail {
				t.Fatalf("Reason = %v, want %v", res.Reason, ReasonTornTail)
			}
			if res.LastGoodOffset != goodLen {
				t.Fatalf("LastGoodOffset = %d, want %d", res.LastGoodOffset, goodLen)
			}
			if res.TruncatedBytes != int64(tc.k) {
				t.Fatalf("TruncatedBytes = %d, want %d", res.TruncatedBytes, tc.k)
			}
			if got := fileSize(t, path); got != goodLen {
				t.Fatalf("file size after recovery = %d, want %d", got, goodLen)
			}

			if got := replayCollect(t, m); len(got) != n {
				t.Fatalf("replayed %d records, want %d", len(got), n)
			}

			// Resume at the truncated EOF: append, reopen, expect n+1 records.
			if err := m.Append(fixedRecord(n)); err != nil {
				t.Fatalf("Append after recovery: %v", err)
			}
			if err := m.Close(); err != nil {
				t.Fatalf("Close: %v", err)
			}

			m2, err := NewManager(path)
			if err != nil {
				t.Fatalf("reopen NewManager: %v", err)
			}
			defer m2.Close()
			if res := m2.Recovery(); res.Reason != ReasonClean {
				t.Fatalf("reopen Reason = %v, want %v", res.Reason, ReasonClean)
			}
			got := replayCollect(t, m2)
			if len(got) != n+1 {
				t.Fatalf("after resume replayed %d records, want %d", len(got), n+1)
			}
			for i := range got {
				if !bytes.Equal(got[i].Payload, fixedRecord(i).Payload) {
					t.Fatalf("record %d payload = %q, want %q", i, got[i].Payload, fixedRecord(i).Payload)
				}
			}
		})
	}
}

// TestRecoverInteriorCorruption corrupts a record partway through the log and checks
// recovery truncates to the last good record before it and flags corruption.
func TestRecoverInteriorCorruption(t *testing.T) {
	const n = 8
	const badIdx = 3
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.log")
	writeRecords(t, path, n)

	fs := frameSize(fixedRecord(0))
	// Flip a byte inside record badIdx's payload.
	corruptOffset := int64(badIdx)*fs + int64(RecordHeaderSize)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	data[corruptOffset] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	res := m.Recovery()
	if res.Reason != ReasonCorrupt {
		t.Fatalf("Reason = %v, want %v", res.Reason, ReasonCorrupt)
	}
	if want := int64(badIdx) * fs; res.LastGoodOffset != want {
		t.Fatalf("LastGoodOffset = %d, want %d", res.LastGoodOffset, want)
	}
	if want := int64(n-badIdx) * fs; res.TruncatedBytes != want {
		t.Fatalf("TruncatedBytes = %d, want %d", res.TruncatedBytes, want)
	}
	if got := fileSize(t, path); got != int64(badIdx)*fs {
		t.Fatalf("file size after recovery = %d, want %d", got, int64(badIdx)*fs)
	}

	got := replayCollect(t, m)
	if len(got) != badIdx {
		t.Fatalf("replayed %d records, want %d", len(got), badIdx)
	}
}

// TestRecoverCleanLog confirms a well-formed log is left untouched.
func TestRecoverCleanLog(t *testing.T) {
	const n = 4
	dir := t.TempDir()
	path := filepath.Join(dir, "clean.log")
	writeRecords(t, path, n)
	sizeBefore := fileSize(t, path)

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer m.Close()

	res := m.Recovery()
	if res.Reason != ReasonClean {
		t.Fatalf("Reason = %v, want %v", res.Reason, ReasonClean)
	}
	if res.TruncatedBytes != 0 {
		t.Fatalf("TruncatedBytes = %d, want 0", res.TruncatedBytes)
	}
	if res.LastGoodOffset != sizeBefore {
		t.Fatalf("LastGoodOffset = %d, want %d", res.LastGoodOffset, sizeBefore)
	}
	if got := fileSize(t, path); got != sizeBefore {
		t.Fatalf("file size changed: %d, want %d", got, sizeBefore)
	}
}

// TestRecoverMissingFile confirms recoverLog is a no-op and doesn't create the file.
func TestRecoverMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.log")

	res, err := recoverLog(path)
	if err != nil {
		t.Fatalf("recoverLog on missing file: %v", err)
	}
	if res.Reason != ReasonClean || res.TruncatedBytes != 0 || res.LastGoodOffset != 0 {
		t.Fatalf("recoverLog on missing file: result = %+v, want zeroed clean", res)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("recoverLog created the file; Stat err = %v, want ErrNotExist", err)
	}
}

// TestRecoverEmptyFile confirms an existing zero-length log recovers cleanly.
func TestRecoverEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatalf("create empty file: %v", err)
	}

	res, err := recoverLog(path)
	if err != nil {
		t.Fatalf("recoverLog on empty file: %v", err)
	}
	if res.Reason != ReasonClean || res.TruncatedBytes != 0 || res.LastGoodOffset != 0 {
		t.Fatalf("recoverLog on empty file: result = %+v, want zeroed clean", res)
	}
}

// Shared between the parent and child halves of the subprocess crash test.
const (
	crashPathVar      = "WAL_CRASH_PATH" // set => this process is the crashing child
	crashGoodRecords  = 5                // records the child commits before crashing
	crashExitCode     = 1                // the child's simulated-crash exit code
	crashSetupErrCode = 2                // child setup failure (a real bug)
)

// TestCrashRecovery kills a real process mid-write via os.Exit, then recovers and resumes
// in a fresh one. With WAL_CRASH_PATH set this process is the crashing child; otherwise
// it is the parent that spawns the child and asserts the log is consistent and resumable.
func TestCrashRecovery(t *testing.T) {
	if path := os.Getenv(crashPathVar); path != "" {
		crashChild(path) // never returns
		return
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "crash.log")

	// Re-exec just this test with the env guard set, so it takes the child branch.
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashRecovery$")
	cmd.Env = append(os.Environ(), crashPathVar+"="+path)
	out, err := cmd.CombinedOutput()

	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("child did not exit non-zero (err=%v)\nchild output:\n%s", err, out)
	}
	if ee.ExitCode() != crashExitCode {
		t.Fatalf("child exit code = %d, want %d\nchild output:\n%s", ee.ExitCode(), crashExitCode, out)
	}

	fs := frameSize(fixedRecord(0))

	m, err := NewManager(path)
	if err != nil {
		t.Fatalf("NewManager after crash: %v\nchild output:\n%s", err, out)
	}
	res := m.Recovery()
	if res.Reason != ReasonTornTail {
		t.Fatalf("recovery Reason = %v, want %v\nchild output:\n%s", res.Reason, ReasonTornTail, out)
	}
	if want := int64(crashGoodRecords) * fs; res.LastGoodOffset != want {
		t.Fatalf("LastGoodOffset = %d, want %d", res.LastGoodOffset, want)
	}

	got := replayCollect(t, m)
	if len(got) != crashGoodRecords {
		t.Fatalf("replayed %d records after crash, want %d", len(got), crashGoodRecords)
	}
	for i := range got {
		if !bytes.Equal(got[i].Payload, fixedRecord(i).Payload) {
			t.Fatalf("record %d payload = %q, want %q", i, got[i].Payload, fixedRecord(i).Payload)
		}
	}

	// Resumable: appending after recovery and reopening must yield one more record.
	if err := m.Append(fixedRecord(crashGoodRecords)); err != nil {
		t.Fatalf("Append after crash recovery: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	m2, err := NewManager(path)
	if err != nil {
		t.Fatalf("reopen after resume: %v", err)
	}
	defer m2.Close()
	if got := replayCollect(t, m2); len(got) != crashGoodRecords+1 {
		t.Fatalf("after resume replayed %d records, want %d", len(got), crashGoodRecords+1)
	}
}

// crashChild commits crashGoodRecords records, writes half of the next frame, and exits
// abruptly without closing.
func crashChild(path string) {
	m, err := NewManager(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child NewManager: %v\n", err)
		os.Exit(crashSetupErrCode)
	}
	for i := 0; i < crashGoodRecords; i++ {
		if err := m.Append(fixedRecord(i)); err != nil {
			fmt.Fprintf(os.Stderr, "child Append #%d: %v\n", i, err)
			os.Exit(crashSetupErrCode)
		}
	}

	full, err := fixedRecord(crashGoodRecords).Encode()
	if err != nil {
		fmt.Fprintf(os.Stderr, "child Encode: %v\n", err)
		os.Exit(crashSetupErrCode)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "child open for partial write: %v\n", err)
		os.Exit(crashSetupErrCode)
	}
	if _, err := f.Write(full[:len(full)/2]); err != nil {
		fmt.Fprintf(os.Stderr, "child partial write: %v\n", err)
		os.Exit(crashSetupErrCode)
	}
	// Intentionally do not Close or Sync: mimic an abrupt process kill mid-write.
	os.Exit(crashExitCode)
}

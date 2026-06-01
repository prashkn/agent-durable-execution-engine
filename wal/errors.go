package wal

import "errors"

// ErrCorruptRecord is returned when a record's stored CRC32 does not match the
// checksum recomputed over its body, or when a record's framing is internally
// inconsistent (e.g. an out-of-range length field). It signals that the on-disk
// bytes can no longer be trusted at that point in the log.
//
// Per the project's "no silent corruption" invariant, a record that fails its
// checksum is never partially trusted: detection halts forward decoding. Truncating
// the log back to the last good record is the job of crash recovery (L3); L2's
// responsibility is only to detect and surface the corruption.
var ErrCorruptRecord = errors.New("wal: corrupt record")

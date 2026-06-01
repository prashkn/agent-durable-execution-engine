package wal

import "errors"

// ErrCorruptRecord means a record's CRC failed or its framing is inconsistent
var ErrCorruptRecord = errors.New("wal: corrupt record")

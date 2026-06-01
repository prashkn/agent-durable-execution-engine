package wal

import "fmt"

// On-disk record layout: [length:u32 LE][crc32:u32 LE][type:u8][payload].
// length covers the body (type + payload); crc32 is over the body only.
const (
	LengthFieldSize  = 4
	CRCFieldSize     = 4
	RecordHeaderSize = LengthFieldSize + CRCFieldSize + 1

	// A corrupt length field could otherwise drive a huge allocation before the CRC
	// is checked, so cap the payload well below what the u32 length can address.
	MaxPayloadSize uint32 = 16 << 20 // 16 MiB
)

// TODO: Add more record types.
const (
	RecordTypeRaw uint8 = 0x01
)

// RecoveryReason says how the startup recovery pass ended its scan. The two truncating
// reasons are distinguished only so callers can stay quiet on a benign torn tail but
// surface genuine corruption.
type RecoveryReason uint8

const (
	ReasonClean    RecoveryReason = iota // decoded to EOF; nothing truncated
	ReasonTornTail                       // partial tail record truncated (crash mid-append)
	ReasonCorrupt                        // bad checksum/framing; truncated to last good record
)

func (r RecoveryReason) String() string {
	switch r {
	case ReasonClean:
		return "clean"
	case ReasonTornTail:
		return "torn-tail"
	case ReasonCorrupt:
		return "corrupt"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(r))
	}
}

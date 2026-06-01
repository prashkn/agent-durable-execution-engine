package wal

// On-disk record layout:
//
//	[length:uint32 little-endian][crc32:uint32 little-endian][type:uint8][payload:length-1 bytes]
//
// length is the size of the body (type + payload), not including itself or the crc.
// crc32 is the IEEE CRC32 of the body bytes (type + payload). It does not cover the
// length or crc fields themselves; a corrupted length is caught instead by the body
// read mismatching (short read) or by the recomputed checksum failing.
const (
	// LengthFieldSize is the width of the leading little-endian length field.
	LengthFieldSize = 4
	// CRCFieldSize is the width of the little-endian CRC32 field that follows length.
	CRCFieldSize = 4
	// RecordHeaderSize is the fixed on-disk header: length + crc + type tag.
	RecordHeaderSize = LengthFieldSize + CRCFieldSize + 1

	// MaxPayloadSize bounds a single record's payload. The 4-byte length field can
	// address ~4 GiB, but we never trust it that far: the CRC covers the body, not
	// the length prefix, so a single flipped bit in the length of a corrupt record
	// could otherwise drive a multi-gigabyte allocation before the checksum is even
	// computed. Capping at a realistic ceiling turns that into an immediate
	// ErrCorruptRecord instead of an OOM. Raise this if a real record ever needs to.
	MaxPayloadSize uint32 = 16 << 20 // 16 MiB
)

// TODO: Add more record types.
const (
	RecordTypeRaw uint8 = 0x01
)

package wal

// On-disk record layout:
//
//	[length:uint32 little-endian][type:uint8][payload:length-1 bytes]
//
// length is the size of (type + payload), not including itself.
const (
	RecordHeaderSize        = 5
	MaxPayloadSize   uint32 = 0xFFFFFFFE
)

// TODO: Add more record types.
const (
	RecordTypeRaw uint8 = 0x01
)

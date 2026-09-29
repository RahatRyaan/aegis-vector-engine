package wal

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

const (
	RecordTypePut byte = 1
	RecordTypeDel byte = 2

	RecordHeaderSize = 25 // CRC(4) + Seq(8) + Type(1) + KeyLen(4) + ValLen(4) + Flags(4)
)

var (
	ErrCorruptRecord = errors.New("wal: checksum mismatch or corrupted record")
	ErrIncompleteRecord = errors.New("wal: incomplete record at log tail")
	castagnoliTable = crc32.MakeTable(crc32.Castagnoli)
)

type Record struct {
	Seq    uint64
	Type   byte
	Key    []byte
	Value  []byte
	Offset uint64
}

func EncodeRecord(seq uint64, recordType byte, key, value []byte) []byte {
	kLen := uint32(len(key))
	vLen := uint32(len(value))
	totalLen := RecordHeaderSize + kLen + vLen

	buf := make([]byte, totalLen)
	// Header
	binary.LittleEndian.PutUint64(buf[4:12], seq)
	buf[12] = recordType
	binary.LittleEndian.PutUint32(buf[13:17], kLen)
	binary.LittleEndian.PutUint32(buf[17:21], vLen)
	binary.LittleEndian.PutUint32(buf[21:25], 0) // Flags / reserved

	// Body
	copy(buf[RecordHeaderSize:RecordHeaderSize+kLen], key)
	copy(buf[RecordHeaderSize+kLen:], value)

	// Calculate CRC32 of payload (bytes 4 to end)
	checksum := crc32.Checksum(buf[4:], castagnoliTable)
	binary.LittleEndian.PutUint32(buf[0:4], checksum)

	return buf
}

func DecodeRecord(buf []byte) (*Record, int, error) {
	if len(buf) < RecordHeaderSize {
		return nil, 0, ErrIncompleteRecord
	}

	checksum := binary.LittleEndian.Uint32(buf[0:4])
	seq := binary.LittleEndian.Uint64(buf[4:12])
	recType := buf[12]
	kLen := binary.LittleEndian.Uint32(buf[13:17])
	vLen := binary.LittleEndian.Uint32(buf[17:21])

	totalLen := int(RecordHeaderSize + kLen + vLen)
	if len(buf) < totalLen {
		return nil, 0, ErrIncompleteRecord
	}

	actualChecksum := crc32.Checksum(buf[4:totalLen], castagnoliTable)
	if checksum != actualChecksum {
		return nil, 0, ErrCorruptRecord
	}

	key := make([]byte, kLen)
	copy(key, buf[RecordHeaderSize:RecordHeaderSize+kLen])

	val := make([]byte, vLen)
	copy(val, buf[RecordHeaderSize+kLen:totalLen])

	return &Record{
		Seq:   seq,
		Type:  recType,
		Key:   key,
		Value: val,
	}, totalLen, nil
}

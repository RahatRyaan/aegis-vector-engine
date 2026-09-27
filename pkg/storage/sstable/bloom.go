package sstable

import (
	"hash/fnv"
)

type BloomFilter struct {
	bitsPerKey int
	bitArray   []byte
	numHashes  int
}

func NewBloomFilter(numKeys int, bitsPerKey int) *BloomFilter {
	if bitsPerKey <= 0 {
		bitsPerKey = 10 // ~1% false positive rate
	}
	if numKeys <= 0 {
		numKeys = 64
	}

	numBits := numKeys * bitsPerKey
	if numBits < 64 {
		numBits = 64
	}
	numBytes := (numBits + 7) / 8

	numHashes := int(float64(bitsPerKey) * 0.69) // ln(2) approx 0.69
	if numHashes < 1 {
		numHashes = 1
	}
	if numHashes > 30 {
		numHashes = 30
	}

	return &BloomFilter{
		bitsPerKey: bitsPerKey,
		bitArray:   make([]byte, numBytes),
		numHashes:  numHashes,
	}
}

func hashKey(key []byte) (uint32, uint32) {
	h := fnv.New64a()
	_, _ = h.Write(key)
	sum := h.Sum64()
	return uint32(sum), uint32(sum >> 32)
}

func (bf *BloomFilter) Add(key []byte) {
	h1, h2 := hashKey(key)
	totalBits := uint32(len(bf.bitArray) * 8)

	for i := 0; i < bf.numHashes; i++ {
		bitPos := (h1 + uint32(i)*h2) % totalBits
		bf.bitArray[bitPos/8] |= 1 << (bitPos % 8)
	}
}

func (bf *BloomFilter) MayContain(key []byte) bool {
	if len(bf.bitArray) == 0 {
		return true
	}
	h1, h2 := hashKey(key)
	totalBits := uint32(len(bf.bitArray) * 8)

	for i := 0; i < bf.numHashes; i++ {
		bitPos := (h1 + uint32(i)*h2) % totalBits
		if (bf.bitArray[bitPos/8] & (1 << (bitPos % 8))) == 0 {
			return false
		}
	}
	return true
}

func (bf *BloomFilter) Bytes() []byte {
	out := make([]byte, 4+len(bf.bitArray))
	out[0] = byte(bf.numHashes)
	out[1] = byte(bf.bitsPerKey)
	copy(out[4:], bf.bitArray)
	return out
}

func LoadBloomFilter(b []byte) *BloomFilter {
	if len(b) < 4 {
		return nil
	}
	return &BloomFilter{
		numHashes:  int(b[0]),
		bitsPerKey: int(b[1]),
		bitArray:   b[4:],
	}
}

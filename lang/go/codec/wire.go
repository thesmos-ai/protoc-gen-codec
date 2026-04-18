// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import (
	"fmt"
	"math/bits"
)

const maxVarintBytes = 10

func Sov(x uint64) int {
	return (bits.Len64(x|1) + 6) / 7
}

func EncodeVarint(buf []byte, x uint64) int {
	i := 0
	for x >= 0x80 {
		buf[i] = byte(x) | 0x80
		x >>= 7
		i++
	}
	buf[i] = byte(x)
	return i + 1
}

func DecodeVarint(data []byte) (uint64, int) {
	if len(data) > 0 && data[0] < 0x80 {
		return uint64(data[0]), 1
	}
	var x uint64
	var s uint
	for i := 0; i < len(data) && i < maxVarintBytes; i++ {
		b := data[i]
		if b < 0x80 {
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, -1
}

// ZigzagEncode32 encodes a signed int32 to a uint32 varint using zigzag.
func ZigzagEncode32(v int32) uint32 {
	return uint32(v<<1) ^ uint32(v>>31)
}

// ZigzagEncode64 encodes a signed int64 to a uint64 varint using zigzag.
func ZigzagEncode64(v int64) uint64 {
	return uint64(v<<1) ^ uint64(v>>63)
}

// ZigzagDecode32 decodes a zigzag-encoded uint32 varint to signed int32.
func ZigzagDecode32(v uint32) int32 {
	return int32(v>>1) ^ -int32(v&1)
}

// ZigzagDecode64 decodes a zigzag-encoded uint64 varint to signed int64.
func ZigzagDecode64(v uint64) int64 {
	return int64(v>>1) ^ -int64(v&1)
}

func SkipField(data []byte, wireType uint64) (int, error) {
	switch wireType {
	case 0:
		_, n := DecodeVarint(data)
		if n < 0 {
			return 0, ErrInvalidVarint
		}
		return n, nil
	case 1:
		if len(data) < 8 {
			return 0, ErrBufferTooShort
		}
		return 8, nil
	case 2:
		l, n := DecodeVarint(data)
		if n < 0 {
			return 0, ErrInvalidVarint
		}
		if uint64(len(data)-n) < l {
			return 0, ErrBufferTooShort
		}
		return n + int(l), nil
	case 5:
		if len(data) < 4 {
			return 0, ErrBufferTooShort
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("wire type %d: %w", wireType, ErrInvalidWireType)
	}
}

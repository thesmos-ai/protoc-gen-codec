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

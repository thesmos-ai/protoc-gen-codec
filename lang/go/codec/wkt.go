// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import "time"

// SizeTimestamp returns the wire-body size (excluding outer tag+length) for
// the given time.Time encoded as google.protobuf.Timestamp. The time.Time
// zero value encodes as an empty body (size 0) so the outer emitter's
// presence guard and the TestTimestamp_ZeroRoundtrips invariant hold.
func SizeTimestamp(t time.Time) int {
	if t.IsZero() {
		return 0
	}
	secs := t.Unix()
	nanos := int32(t.Nanosecond())
	return sizeTSBody(secs, nanos)
}

// SizeDuration returns the wire-body size for a time.Duration encoded as
// google.protobuf.Duration.
func SizeDuration(d time.Duration) int {
	secs := int64(d / time.Second)
	nanos := int32(d % time.Second)
	return sizeTSBody(secs, nanos)
}

func sizeTSBody(secs int64, nanos int32) int {
	n := 0
	if secs != 0 {
		n += 1 + Sov(uint64(secs))
	}
	if nanos != 0 {
		n += 1 + Sov(uint64(nanos))
	}
	return n
}

// EncodeTimestamp writes the Timestamp wire body into buf; returns bytes
// written. buf must be at least SizeTimestamp(t) bytes. A zero-value
// time.Time writes 0 bytes, matching SizeTimestamp.
func EncodeTimestamp(buf []byte, t time.Time) int {
	if t.IsZero() {
		return 0
	}
	return encodeTSBody(buf, t.Unix(), int32(t.Nanosecond()))
}

// EncodeDuration writes the Duration wire body into buf.
func EncodeDuration(buf []byte, d time.Duration) int {
	return encodeTSBody(buf, int64(d/time.Second), int32(d%time.Second))
}

func encodeTSBody(buf []byte, secs int64, nanos int32) int {
	n := 0
	if secs != 0 {
		buf[n] = 0x08 // field 1, varint
		n++
		n += EncodeVarint(buf[n:], uint64(secs))
	}
	if nanos != 0 {
		buf[n] = 0x10 // field 2, varint
		n++
		n += EncodeVarint(buf[n:], uint64(nanos))
	}
	return n
}

// DecodeTimestamp parses a Timestamp wire body into time.Time (UTC).
// When both seconds and nanos are zero (or the body is empty) the function
// returns the time.Time{} zero value rather than the Unix epoch, so a
// roundtrip of a zero-value struct field yields the same zero value.
func DecodeTimestamp(data []byte) (time.Time, error) {
	secs, nanos, err := decodeTSBody(data)
	if err != nil {
		return time.Time{}, err
	}
	if secs == 0 && nanos == 0 {
		return time.Time{}, nil
	}
	return time.Unix(secs, int64(nanos)).UTC(), nil
}

// DecodeDuration parses a Duration wire body into time.Duration.
func DecodeDuration(data []byte) (time.Duration, error) {
	secs, nanos, err := decodeTSBody(data)
	if err != nil {
		return 0, err
	}
	return time.Duration(secs)*time.Second + time.Duration(nanos), nil
}

func decodeTSBody(data []byte) (int64, int32, error) {
	var secs int64
	var nanos int32
	i := 0
	l := len(data)
	for i < l {
		tag, tn := DecodeVarint(data[i:])
		if tn < 0 {
			return 0, 0, ErrInvalidVarint
		}
		i += tn
		switch tag >> 3 {
		case 1:
			v, vn := DecodeVarint(data[i:])
			if vn < 0 {
				return 0, 0, ErrInvalidVarint
			}
			i += vn
			secs = int64(v)
		case 2:
			v, vn := DecodeVarint(data[i:])
			if vn < 0 {
				return 0, 0, ErrInvalidVarint
			}
			i += vn
			nanos = int32(v)
		default:
			sn, err := SkipField(data[i:], tag&0x7)
			if err != nil {
				return 0, 0, err
			}
			i += sn
		}
	}
	return secs, nanos, nil
}

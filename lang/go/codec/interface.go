// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

// Sizer is the method set for computing the serialized size of a codec
// target without allocating or encoding. Separated from Marshaler so
// callers that only need size information (metrics, buffer preallocation)
// can accept a minimal contract.
type Sizer interface {
	SizeCodec() int
}

// Marshaler is the method set for encoding a codec target to proto3 wire
// format. Marshaler embeds Sizer because MarshalCodec depends on
// SizeCodec for buffer allocation; callers that want to encode always
// also need size.
type Marshaler interface {
	Sizer
	MarshalCodec() ([]byte, error)
	MarshalToCodec(buf []byte) (int, error)
}

// Unmarshaler is the method set for decoding proto3 wire bytes into a
// codec target. Separated from Marshaler so streaming decoders and
// replay consumers can accept a decode-only contract.
type Unmarshaler interface {
	UnmarshalCodec(data []byte) error
}

// Resetter is the method set for returning a codec target to its zero
// wire state so it can be returned to a sync.Pool. Separated so pool
// wrappers can accept a reset-only contract.
type Resetter interface {
	ResetCodec()
}

// Codec is the full method set emitted on every codec-annotated type.
// Callers needing every behavior accept Codec; narrower callers accept
// one of the sub-interfaces (Sizer, Marshaler, Unmarshaler, Resetter).
type Codec interface {
	Marshaler
	Unmarshaler
	Resetter
}

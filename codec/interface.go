// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

// Marshaler is the unified serialization interface for types with
// generated codec methods.
type Marshaler interface {
	SizeCodec() int
	MarshalCodec() ([]byte, error)
	MarshalToCodec([]byte) (int, error)
	UnmarshalCodec([]byte) error
	ResetCodec()
}

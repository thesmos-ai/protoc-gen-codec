// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package codec

import "errors"

var (
	ErrInvalidLength   = errors.New("codec: invalid wire length")
	ErrInvalidWireType = errors.New("codec: invalid wire type")
	ErrInvalidTag      = errors.New("codec: invalid tag")
	ErrInvalidVarint   = errors.New("codec: invalid varint")
	ErrBufferTooShort  = errors.New("codec: buffer too short")
)

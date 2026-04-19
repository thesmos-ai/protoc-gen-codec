// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	optGoType     protowire.Number = 50001
	optGoField    protowire.Number = 50002
	optGoCast     protowire.Number = 50003
	optFixedLen   protowire.Number = 50004
	optKeepCap    protowire.Number = 50005
	optUsePointer protowire.Number = 50006
	optOneof      protowire.Number = 50007
)

// OneofConfig mirrors the codec.OneofConfig submessage used on
// MessageOptions to declare how a non-synthetic proto3 oneof maps onto
// the target Go type.
type OneofConfig struct {
	Name          string // proto oneof name
	Discriminator string // Go struct field name holding the active-branch enum
	Cast          string // Go type name of the discriminator
}

// messageOneofs extracts every codec.oneof entry from a message's
// options. Returns nil if the message has no codec.oneof annotation.
func messageOneofs(msg *protogen.Message) []OneofConfig {
	opts := msg.Desc.Options()
	if opts == nil {
		return nil
	}
	raw := opts.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return nil
	}
	var out []OneofConfig
	for len(raw) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return out
		}
		raw = raw[n:]
		if fnum != optOneof || wtype != protowire.BytesType {
			vn := consumeFieldValue(raw, wtype)
			if vn < 0 {
				return out
			}
			raw = raw[vn:]
			continue
		}
		body, bn := protowire.ConsumeBytes(raw)
		if bn < 0 {
			return out
		}
		raw = raw[bn:]
		cfg, ok := parseOneofConfig(body)
		if !ok {
			return out
		}
		out = append(out, cfg)
	}
	return out
}

// parseOneofConfig decodes an OneofConfig submessage body. Returns
// (zero, false) on malformed input.
func parseOneofConfig(body []byte) (OneofConfig, bool) {
	var cfg OneofConfig
	for len(body) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(body)
		if n < 0 {
			return OneofConfig{}, false
		}
		body = body[n:]
		if wtype != protowire.BytesType {
			vn := consumeFieldValue(body, wtype)
			if vn < 0 {
				return OneofConfig{}, false
			}
			body = body[vn:]
			continue
		}
		val, vn := protowire.ConsumeBytes(body)
		if vn < 0 {
			return OneofConfig{}, false
		}
		body = body[vn:]
		//nolint:exhaustive // OneofConfig has three known fields (name, discriminator, cast); unknown field numbers are intentionally ignored so newer/older schemas coexist.
		switch fnum {
		case 1:
			cfg.Name = string(val)
		case 2:
			cfg.Discriminator = string(val)
		case 3:
			cfg.Cast = string(val)
		}
	}
	return cfg, true
}

func messageGoType(msg *protogen.Message) string {
	return extractString(msg.Desc.Options(), optGoType)
}

func fieldGoField(field *protogen.Field) string {
	return extractString(field.Desc.Options(), optGoField)
}

func fieldGoCast(field *protogen.Field) string {
	return extractString(field.Desc.Options(), optGoCast)
}

func fieldFixedLen(field *protogen.Field) (uint32, bool) {
	return extractUint32(field.Desc.Options(), optFixedLen)
}

func fieldKeepCapacity(field *protogen.Field) bool {
	v, _ := extractBool(field.Desc.Options(), optKeepCap)
	return v
}

func fieldUsePointer(field *protogen.Field) (bool, bool) {
	return extractBool(field.Desc.Options(), optUsePointer)
}

func extractString(pm protoreflect.ProtoMessage, num protowire.Number) string {
	if pm == nil {
		return ""
	}
	raw := pm.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return ""
	}
	for len(raw) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return ""
		}
		raw = raw[n:]
		switch wtype {
		case protowire.BytesType:
			val, vn := protowire.ConsumeBytes(raw)
			if vn < 0 {
				return ""
			}
			raw = raw[vn:]
			if fnum == num {
				return string(val)
			}
		default:
			vn := consumeFieldValue(raw, wtype)
			if vn < 0 {
				return ""
			}
			raw = raw[vn:]
		}
	}
	return ""
}

func extractUint32(pm protoreflect.ProtoMessage, num protowire.Number) (uint32, bool) {
	if pm == nil {
		return 0, false
	}
	raw := pm.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return 0, false
	}
	for len(raw) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return 0, false
		}
		raw = raw[n:]
		switch wtype {
		case protowire.VarintType:
			val, vn := protowire.ConsumeVarint(raw)
			if vn < 0 {
				return 0, false
			}
			raw = raw[vn:]
			if fnum == num {
				return uint32(val), true
			}
		default:
			vn := consumeFieldValue(raw, wtype)
			if vn < 0 {
				return 0, false
			}
			raw = raw[vn:]
		}
	}
	return 0, false
}

func extractBool(pm protoreflect.ProtoMessage, num protowire.Number) (bool, bool) {
	if pm == nil {
		return false, false
	}
	raw := pm.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return false, false
	}
	for len(raw) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return false, false
		}
		raw = raw[n:]
		switch wtype {
		case protowire.VarintType:
			val, vn := protowire.ConsumeVarint(raw)
			if vn < 0 {
				return false, false
			}
			raw = raw[vn:]
			if fnum == num {
				return val != 0, true
			}
		default:
			vn := consumeFieldValue(raw, wtype)
			if vn < 0 {
				return false, false
			}
			raw = raw[vn:]
		}
	}
	return false, false
}

func consumeFieldValue(raw []byte, wtype protowire.Type) int {
	switch wtype {
	case protowire.VarintType:
		_, n := protowire.ConsumeVarint(raw)
		return n
	case protowire.Fixed32Type:
		if len(raw) < 4 {
			return -1
		}
		return 4
	case protowire.Fixed64Type:
		if len(raw) < 8 {
			return -1
		}
		return 8
	case protowire.BytesType:
		_, n := protowire.ConsumeBytes(raw)
		return n
	default:
		return -1
	}
}

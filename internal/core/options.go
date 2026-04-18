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
)

func messageGoType(msg *protogen.Message) string {
	v, _ := extractString(msg.Desc.Options(), optGoType)
	return v
}

func fieldGoField(field *protogen.Field) string {
	v, _ := extractString(field.Desc.Options(), optGoField)
	return v
}

func fieldGoCast(field *protogen.Field) string {
	v, _ := extractString(field.Desc.Options(), optGoCast)
	return v
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

func extractString(pm protoreflect.ProtoMessage, num protowire.Number) (string, bool) {
	if pm == nil {
		return "", false
	}
	raw := pm.ProtoReflect().GetUnknown()
	if len(raw) == 0 {
		return "", false
	}
	for len(raw) > 0 {
		fnum, wtype, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return "", false
		}
		raw = raw[n:]
		switch wtype {
		case protowire.BytesType:
			val, vn := protowire.ConsumeBytes(raw)
			if vn < 0 {
				return "", false
			}
			raw = raw[vn:]
			if fnum == num {
				return string(val), true
			}
		default:
			vn := consumeFieldValue(raw, wtype)
			if vn < 0 {
				return "", false
			}
			raw = raw[vn:]
		}
	}
	return "", false
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

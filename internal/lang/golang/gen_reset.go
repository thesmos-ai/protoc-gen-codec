// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"go.stealthscale.io/protoc-gen-codec/internal/core"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func generateResetCodec(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	g.P("func (m *", info.TargetType, ") ResetCodec() {")
	g.P("if m == nil {")
	g.P("return")
	g.P("}")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldReset(g, fileMap, f)
	}

	g.P("if ri, ok := any(m).(interface{ ResetInternal() }); ok {")
	g.P("ri.ResetInternal()")
	g.P("}")

	g.P("}")
}

func generateFieldReset(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) {
	accessor := "m." + f.TargetName

	// WKT dispatch must precede IsMessage/IsMap checks; see generateFieldMarshal.
	if f.WellKnown == core.WKTTimestamp {
		g.P(accessor, " = ", identTimeTime, "{}")
		return
	}
	if f.WellKnown == core.WKTDuration {
		g.P(accessor, " = 0")
		return
	}

	if f.IsMap {
		if f.KeepCapacity {
			g.P("clear(", accessor, ")")
		} else {
			g.P(accessor, " = nil")
		}
		return
	}

	if f.IsRepeated {
		if f.KeepCapacity {
			g.P(accessor, " = ", accessor, "[:0]")
		} else {
			g.P(accessor, " = nil")
		}
		return
	}

	if f.IsMessage {
		if f.UsePointer {
			g.P(accessor, " = nil")
		} else {
			msgType := goIdentForMessage(g, fileMap, f)
			g.P(accessor, " = ", msgType, "{}")
		}
		return
	}

	if f.IsProto3Optional {
		g.P(accessor, " = nil")
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := goCastName(g, fileMap, f)
		g.P(accessor, " = ", zeroType, "{}")

	case f.IsString:
		g.P(accessor, ` = ""`)

	case f.IsBytes:
		if f.KeepCapacity {
			g.P(accessor, " = ", accessor, "[:0]")
		} else {
			g.P(accessor, " = nil")
		}

	case f.ProtoKind == protoreflect.BoolKind:
		g.P(accessor, " = false")

	default:
		g.P(accessor, " = 0")
	}
}

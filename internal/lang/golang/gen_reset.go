// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"go.stealthscale.io/protoc-gen-codec/internal/core"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func generateResetCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.GoType, ") ResetCodec() {")
	g.P("if m == nil {")
	g.P("return")
	g.P("}")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldReset(g, f)
	}

	g.P("if ri, ok := any(m).(interface{ ResetInternal() }); ok {")
	g.P("ri.ResetInternal()")
	g.P("}")

	g.P("}")
}

func generateFieldReset(g *protogen.GeneratedFile, f *core.FieldInfo) {
	accessor := "m." + f.GoName

	if f.IsRepeated {
		if f.KeepCapacity {
			g.P(accessor, " = ", accessor, "[:0]")
		} else {
			g.P(accessor, " = nil")
		}
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := f.QualifiedZeroType(g)
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

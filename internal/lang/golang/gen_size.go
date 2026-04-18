// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"go.stealthscale.io/protoc-gen-codec/internal/core"
	"google.golang.org/protobuf/compiler/protogen"
)

func generateSizeCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.GoType, ") SizeCodec() int {")
	g.P("if m == nil {")
	g.P("return 0")
	g.P("}")
	g.P("var n int")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldSize(g, f)
	}

	g.P("return n")
	g.P("}")
}

func generateFieldSize(g *protogen.GeneratedFile, f *core.FieldInfo) {
	ts := core.TagSize(f.ProtoNum)
	accessor := "m." + f.GoName

	if f.IsRepeated {
		generateRepeatedFieldSize(g, f, ts)
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := f.QualifiedZeroType(g)
		g.P("if ", accessor, " != (", zeroType, "{}) {")
		g.P("n += ", ts+core.SovLocal(uint64(f.FixedLen))+int(f.FixedLen))
		g.P("}")

	case f.Wire == core.WireVarint:
		if f.ProtoKind.String() == "bool" {
			g.P("if ", accessor, " {")
			g.P("n += ", ts+1)
		} else {
			g.P("if ", accessor, " != 0 {")
			g.P("n += ", ts, " + ", identSov, "(uint64(", accessor, "))")
		}
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if ", accessor, " != 0 {")
		g.P("n += ", ts+8)
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if ", accessor, " != 0 {")
		g.P("n += ", ts+4)
		g.P("}")

	case f.IsString:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ")")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")

	case f.IsBytes:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ")")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")
	}
}

func generateRepeatedFieldSize(g *protogen.GeneratedFile, f *core.FieldInfo, ts int) {
	accessor := "m." + f.GoName

	switch {
	case f.IsString:
		g.P("for _, s := range ", accessor, " {")
		g.P("l := len(s)")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")

	case f.IsBytes && f.FixedLen > 0:
		perElem := ts + core.SovLocal(uint64(f.FixedLen)) + int(f.FixedLen)
		g.P("n += len(", accessor, ") * ", perElem)

	case f.IsBytes:
		g.P("for _, b := range ", accessor, " {")
		g.P("l := len(b)")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireVarint:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := 0")
		g.P("for _, v := range ", accessor, " {")
		g.P("l += ", identSov, "(uint64(v))")
		g.P("}")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ") * 8")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ") * 4")
		g.P("n += ", ts, " + ", identSov, "(uint64(l)) + l")
		g.P("}")
	}
}

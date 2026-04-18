// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"go.stealthscale.io/protoc-gen-codec/internal/core"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func generateMarshalCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.GoType, ") MarshalCodec() ([]byte, error) {")
	g.P("if m == nil {")
	g.P("return nil, nil")
	g.P("}")
	g.P("size := m.SizeCodec()")
	g.P("if size == 0 {")
	g.P("return nil, nil")
	g.P("}")
	g.P("buf := make([]byte, size)")
	g.P("n, err := m.MarshalToCodec(buf)")
	g.P("if err != nil {")
	g.P("return nil, err")
	g.P("}")
	g.P("return buf[:n], nil")
	g.P("}")
}

func generateMarshalToCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.GoType, ") MarshalToCodec(buf []byte) (int, error) {")
	g.P("if m == nil {")
	g.P("return 0, nil")
	g.P("}")
	g.P("n := 0")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldMarshal(g, f)
	}

	g.P("return n, nil")
	g.P("}")
}

func generateFieldMarshal(g *protogen.GeneratedFile, f *core.FieldInfo) {
	accessor := "m." + f.GoName

	if f.IsRepeated {
		generateRepeatedFieldMarshal(g, f)
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := f.QualifiedZeroType(g)
		g.P("if ", accessor, " != (", zeroType, "{}) {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],", f.FixedLen, ")")
		g.P("copy(buf[n:], ", accessor, "[:])")
		g.P("n += ", f.FixedLen)
		g.P("}")

	case f.Wire == core.WireVarint:
		if f.ProtoKind == protoreflect.BoolKind {
			g.P("if ", accessor, " {")
			emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
			g.P("buf[n] = 1")
			g.P("n++")
		} else {
			g.P("if ", accessor, " != 0 {")
			emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", accessor, "))")
		}
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", accessor, "))")
		g.P("n += 8")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", accessor, "))")
		g.P("n += 4")
		g.P("}")

	case f.IsString:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")))")
		g.P("n += copy(buf[n:], ", accessor, ")")
		g.P("}")

	case f.IsBytes:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")))")
		g.P("n += copy(buf[n:], ", accessor, ")")
		g.P("}")
	}
}

func generateRepeatedFieldMarshal(g *protogen.GeneratedFile, f *core.FieldInfo) {
	accessor := "m." + f.GoName

	switch {
	case f.IsString:
		g.P("for _, s := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(s)))")
		g.P("n += copy(buf[n:], s)")
		g.P("}")

	case f.IsBytes && f.FixedLen > 0:
		g.P("for _, b := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],", f.FixedLen, ")")
		g.P("copy(buf[n:], b[:])")
		g.P("n += ", f.FixedLen)
		g.P("}")

	case f.IsBytes:
		g.P("for _, b := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(b)))")
		g.P("n += copy(buf[n:], b)")
		g.P("}")

	case f.Wire == core.WireVarint:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
		g.P("l := 0")
		g.P("for _, v := range ", accessor, " {")
		g.P("l += ", identSov, "(uint64(v))")
		g.P("}")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(l))")
		g.P("for _, v := range ", accessor, " {")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(v))")
		g.P("}")
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")*8))")
		g.P("for _, v := range ", accessor, " {")
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(v))")
		g.P("n += 8")
		g.P("}")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel, "buf", "n")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")*4))")
		g.P("for _, v := range ", accessor, " {")
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(v))")
		g.P("n += 4")
		g.P("}")
		g.P("}")
	}
}

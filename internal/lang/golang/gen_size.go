// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

func generateSizeCodec(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	g.P("func (m *", info.TargetType, ") SizeCodec() int {")
	g.P("if m == nil {")
	g.P("return 0")
	g.P("}")
	g.P("var n int")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldSize(g, fileMap, f)
	}

	g.P("return n")
	g.P("}")
}

func generateFieldSize(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) {
	ts := core.TagSize(f.ProtoNum)
	accessor := "m." + f.TargetName

	// WKT dispatch must precede IsMessage/IsMap checks; see generateFieldMarshal.
	if f.WellKnown == core.WKTTimestamp {
		g.P("{ var zero ", identTimeTime)
		g.P("if ", accessor, " != zero {")
		g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
		g.P("} }")
		return
	}
	if f.WellKnown == core.WKTDuration {
		g.P("if ", accessor, " != 0 {")
		g.P("sz := ", identSizeDuration, "(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
		g.P("}")
		return
	}

	if f.IsMap {
		generateMapFieldSize(g, f, accessor, ts)
		return
	}

	if f.IsRepeated {
		generateRepeatedFieldSize(g, fileMap, f, ts)
		return
	}

	if f.IsMessage {
		if f.UsePointer {
			// Singular *T: gate on sz > 0 (not just non-nil) so a pooled-but-
			// reset message — preserved as a non-nil pointer with all fields
			// at proto3 default — serializes as absent. Phase 4.10 ResetCodec
			// keeps the *T heap slot for pointer pooling, so the pointer
			// alone is no longer a presence signal; only field content is.
			g.P("if ", accessor, " != nil {")
			g.P("if sz := ", accessor, ".SizeCodec(); sz > 0 {")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
			g.P("}")
			g.P("}")
		} else {
			g.P("if sz := (&", accessor, ").SizeCodec(); sz > 0 {")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
			g.P("}")
		}
		return
	}

	if f.IsProto3Optional {
		derefAccessor := "*" + accessor
		g.P("if ", accessor, " != nil {")
		//nolint:exhaustive // proto3 optional applies only to scalar wire kinds; WireLenDel is handled elsewhere.
		switch f.Wire {
		case core.WireVarint:
			switch f.ProtoKind {
			case protoreflect.BoolKind:
				g.P("n += ", ts+1)
			case protoreflect.Sint32Kind:
				g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", identZigzagEncode32, "(int32(", derefAccessor, "))))")
			case protoreflect.Sint64Kind:
				g.P("n += ", ts, " + ", identSizeVarint, "(", identZigzagEncode64, "(int64(", derefAccessor, ")))")
			default:
				g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", derefAccessor, "))")
			}
		case core.WireFixed64:
			g.P("n += ", ts+8)
		case core.WireFixed32:
			g.P("n += ", ts+4)
		}
		g.P("}")
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := goCastName(g, fileMap, f)
		g.P("if ", accessor, " != (", zeroType, "{}) {")
		g.P("n += ", ts+core.SovLocal(uint64(f.FixedLen))+int(f.FixedLen))
		g.P("}")

	case f.Wire == core.WireVarint:
		switch f.ProtoKind {
		case protoreflect.BoolKind:
			g.P("if ", accessor, " {")
			g.P("n += ", ts+1)
		case protoreflect.Sint32Kind:
			g.P("if ", accessor, " != 0 {")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
		case protoreflect.Sint64Kind:
			g.P("if ", accessor, " != 0 {")
			g.P("n += ", ts, " + ", identSizeVarint, "(", identZigzagEncode64, "(int64(", accessor, ")))")
		default:
			g.P("if ", accessor, " != 0 {")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", accessor, "))")
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

	// String and bytes are wire-identical: both are length-delimited and
	// contribute tag + Sov(len) + len bytes. The distinction matters only in
	// unmarshal (slab vs append copy); size/marshal share one arm.
	case f.IsString || f.IsBytes:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")
	}
}

func generateMapFieldSize(g *protogen.GeneratedFile, f *core.FieldInfo, accessor string, ts int) {
	keySize := scalarSizeExpr(g, f.MapKey, "k")
	valSize := scalarSizeExpr(g, f.MapValue, "v")
	keyTagSize := core.TagSize(f.MapKey.ProtoNum)
	valTagSize := core.TagSize(f.MapValue.ProtoNum)
	g.P("for k, v := range ", accessor, " {")
	g.P("entrySz := ", keyTagSize, " + ", keySize, " + ", valTagSize, " + ", valSize)
	g.P("n += ", ts, " + ", identSizeVarint, "(uint64(entrySz)) + entrySz")
	g.P("}")
}

func generateRepeatedFieldSize(g *protogen.GeneratedFile, _ map[string]*protogen.File, f *core.FieldInfo, ts int) {
	accessor := "m." + f.TargetName

	if f.IsMessage {
		if f.UsePointer {
			g.P("for _, elem := range ", accessor, " {")
			g.P("if elem == nil { continue }")
			g.P("sz := elem.SizeCodec()")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
			g.P("}")
		} else {
			g.P("for idx := range ", accessor, " {")
			g.P("elem := &", accessor, "[idx]")
			g.P("sz := elem.SizeCodec()")
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
			g.P("}")
		}
		return
	}

	switch {
	case f.IsString:
		g.P("for _, s := range ", accessor, " {")
		g.P("l := len(s)")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")

	case f.IsBytes && f.FixedLen > 0:
		perElem := ts + core.SovLocal(uint64(f.FixedLen)) + int(f.FixedLen)
		g.P("n += len(", accessor, ") * ", perElem)

	case f.IsBytes:
		g.P("for _, b := range ", accessor, " {")
		g.P("l := len(b)")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireVarint:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := 0")
		switch f.ProtoKind {
		case protoreflect.Sint32Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSizeVarint, "(uint64(", identZigzagEncode32, "(int32(v))))")
			g.P("}")
		case protoreflect.Sint64Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSizeVarint, "(", identZigzagEncode64, "(int64(v)))")
			g.P("}")
		default:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSizeVarint, "(uint64(v))")
			g.P("}")
		}
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ") * 8")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if len(", accessor, ") > 0 {")
		g.P("l := len(", accessor, ") * 4")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
		g.P("}")
	}
}

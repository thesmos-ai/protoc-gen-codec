// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

func generateMarshalCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	g.P("func (m *", info.TargetType, ") MarshalCodec() ([]byte, error) {")
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

func generateMarshalToCodec(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	g.P("func (m *", info.TargetType, ") MarshalToCodec(buf []byte) (int, error) {")
	g.P("if m == nil {")
	g.P("return 0, nil")
	g.P("}")
	g.P("if len(buf) < m.SizeCodec() {")
	g.P("return 0, ", identErrBufferTooShort)
	g.P("}")
	g.P("n := 0")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldMarshal(g, fileMap, f)
	}

	g.P("return n, nil")
	g.P("}")
}

func generateFieldMarshal(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) {
	accessor := "m." + f.TargetName

	// WKT dispatch must precede IsMessage/IsMap checks: analyzeField cleared
	// those flags for well-known types, but the WKT path has its own
	// zero-value guard (time.Time{} for Timestamp; 0 for Duration) and
	// encode helper, distinct from the generic message path.
	if f.WellKnown == core.WKTTimestamp {
		g.P("{")
		g.P("var zero ", identTimeTime)
		g.P("if ", accessor, " != zero {")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
		g.P("n += ", identEncodeTimestamp, "(buf[n:], ", accessor, ")")
		g.P("}")
		g.P("}")
		return
	}
	if f.WellKnown == core.WKTDuration {
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("sz := ", identSizeDuration, "(", accessor, ")")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
		g.P("n += ", identEncodeDuration, "(buf[n:], ", accessor, ")")
		g.P("}")
		return
	}

	if f.IsMap {
		generateMapFieldMarshal(g, f, accessor)
		return
	}

	if f.IsRepeated {
		generateRepeatedFieldMarshal(g, fileMap, f)
		return
	}

	if f.IsMessage {
		if f.UsePointer {
			// Singular *T: gate on sz > 0 (not just non-nil) for parity with
			// gen_size's presence rule. After Phase 4.10 ResetCodec the *T
			// pointer survives in the receiver for pooling, so an empty
			// (all-zero) reset message must serialize as absent.
			g.P("if ", accessor, " != nil {")
			g.P("if sz := ", accessor, ".SizeCodec(); sz > 0 {")
			emitTag(g, f.ProtoNum, core.WireLenDel)
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("wn, err := ", accessor, ".MarshalToCodec(buf[n:])")
			g.P("if err != nil { return 0, err }")
			g.P("n += wn")
			g.P("}")
			g.P("}")
		} else {
			// Singular value: T, SizeCodec() > 0 used as presence predicate.
			g.P("if sz := (&", accessor, ").SizeCodec(); sz > 0 {")
			emitTag(g, f.ProtoNum, core.WireLenDel)
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("wn, err := (&", accessor, ").MarshalToCodec(buf[n:])")
			g.P("if err != nil { return 0, err }")
			g.P("n += wn")
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
				emitTag(g, f.ProtoNum, f.Wire)
				g.P("if ", derefAccessor, " { buf[n] = 1 } else { buf[n] = 0 }")
				g.P("n++")
			case protoreflect.Sint32Kind:
				emitTag(g, f.ProtoNum, f.Wire)
				g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", identZigzagEncode32, "(int32(", derefAccessor, "))))")
			case protoreflect.Sint64Kind:
				emitTag(g, f.ProtoNum, f.Wire)
				g.P("n += ", identEncodeVarint, "(buf[n:],", identZigzagEncode64, "(int64(", derefAccessor, ")))")
			default:
				emitTag(g, f.ProtoNum, f.Wire)
				g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", derefAccessor, "))")
			}
		case core.WireFixed64:
			emitTag(g, f.ProtoNum, f.Wire)
			g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", derefAccessor, "))")
			g.P("n += 8")
		case core.WireFixed32:
			emitTag(g, f.ProtoNum, f.Wire)
			g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", derefAccessor, "))")
			g.P("n += 4")
		}
		g.P("}")
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := goCastName(g, fileMap, f)
		g.P("if ", accessor, " != (", zeroType, "{}) {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],", f.FixedLen, ")")
		g.P("copy(buf[n:], ", accessor, "[:])")
		g.P("n += ", f.FixedLen)
		g.P("}")

	case f.Wire == core.WireVarint:
		switch f.ProtoKind {
		case protoreflect.BoolKind:
			g.P("if ", accessor, " {")
			emitTag(g, f.ProtoNum, f.Wire)
			g.P("buf[n] = 1")
			g.P("n++")
		case protoreflect.Sint32Kind:
			g.P("if ", accessor, " != 0 {")
			emitTag(g, f.ProtoNum, f.Wire)
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
		case protoreflect.Sint64Kind:
			g.P("if ", accessor, " != 0 {")
			emitTag(g, f.ProtoNum, f.Wire)
			g.P("n += ", identEncodeVarint, "(buf[n:],", identZigzagEncode64, "(int64(", accessor, ")))")
		default:
			g.P("if ", accessor, " != 0 {")
			emitTag(g, f.ProtoNum, f.Wire)
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", accessor, "))")
		}
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", accessor, "))")
		g.P("n += 8")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if ", accessor, " != 0 {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", accessor, "))")
		g.P("n += 4")
		g.P("}")

	// String and bytes are wire-identical: both are length-delimited and
	// emit len-prefix + payload. The distinction matters only in unmarshal
	// (slab vs append copy); at marshal/size level they share one arm.
	case f.IsString || f.IsBytes:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")))")
		g.P("n += copy(buf[n:], ", accessor, ")")
		g.P("}")
	}
}

// generateMapFieldMarshal emits a deterministic map encoder. Keys are visited
// in sorted order so the wire output is byte-stable across calls — important
// for content-addressable hashing, signing, and reproducible builds. Cost:
// one slice allocation per map per Marshal call for ordered key kinds (the
// sorted key slice). Bool keys use an explicit false-then-true sequence and
// stay zero-alloc.
func generateMapFieldMarshal(g *protogen.GeneratedFile, f *core.FieldInfo, accessor string) {
	keySize := scalarSizeExpr(g, f.MapKey, "k")
	valSize := scalarSizeExpr(g, f.MapValue, "v")
	keyTagSize := core.TagSize(f.MapKey.ProtoNum)
	valTagSize := core.TagSize(f.MapValue.ProtoNum)

	emitBody := func() {
		g.P("v := ", accessor, "[k]")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("entrySz := ", keyTagSize, " + ", keySize, " + ", valTagSize, " + ", valSize)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(entrySz))")
		emitScalarWrite(g, f.MapKey, "k")
		emitScalarWrite(g, f.MapValue, "v")
	}

	if f.MapKey.ProtoKind == protoreflect.BoolKind {
		// Two possible keys; emit in canonical false→true order.
		for _, kv := range []string{"false", "true"} {
			g.P("if _, ok := ", accessor, "[", kv, "]; ok {")
			g.P("k := ", kv)
			emitBody()
			g.P("}")
		}
		return
	}

	g.P("for _, k := range ", identSlicesSorted, "(", identMapsKeys, "(", accessor, ")) {")
	emitBody()
	g.P("}")
}

func generateRepeatedFieldMarshal(g *protogen.GeneratedFile, _ map[string]*protogen.File, f *core.FieldInfo) {
	accessor := "m." + f.TargetName

	if f.IsMessage {
		if f.UsePointer {
			// Pointer slice: skip nil entries, marshal via pointer receiver.
			g.P("for _, elem := range ", accessor, " {")
			g.P("if elem == nil { continue }")
			emitTag(g, f.ProtoNum, core.WireLenDel)
			g.P("sz := elem.SizeCodec()")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("wn, err := elem.MarshalToCodec(buf[n:])")
			g.P("if err != nil { return 0, err }")
			g.P("n += wn")
			g.P("}")
		} else {
			// Value slice: take address of element for pointer-receiver methods.
			g.P("for idx := range ", accessor, " {")
			g.P("elem := &", accessor, "[idx]")
			emitTag(g, f.ProtoNum, core.WireLenDel)
			g.P("sz := elem.SizeCodec()")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("wn, err := elem.MarshalToCodec(buf[n:])")
			g.P("if err != nil { return 0, err }")
			g.P("n += wn")
			g.P("}")
		}
		return
	}

	switch {
	case f.IsString:
		g.P("for _, s := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(s)))")
		g.P("n += copy(buf[n:], s)")
		g.P("}")

	case f.IsBytes && f.FixedLen > 0:
		g.P("for _, b := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],", f.FixedLen, ")")
		g.P("copy(buf[n:], b[:])")
		g.P("n += ", f.FixedLen)
		g.P("}")

	case f.IsBytes:
		g.P("for _, b := range ", accessor, " {")
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(b)))")
		g.P("n += copy(buf[n:], b)")
		g.P("}")

	case f.Wire == core.WireVarint:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("l := 0")
		switch f.ProtoKind {
		case protoreflect.Sint32Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSov, "(uint64(", identZigzagEncode32, "(int32(v))))")
			g.P("}")
		case protoreflect.Sint64Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSov, "(", identZigzagEncode64, "(int64(v)))")
			g.P("}")
		default:
			g.P("for _, v := range ", accessor, " {")
			g.P("l += ", identSov, "(uint64(v))")
			g.P("}")
		}
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(l))")
		switch f.ProtoKind {
		case protoreflect.Sint32Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", identZigzagEncode32, "(int32(v))))")
			g.P("}")
		case protoreflect.Sint64Kind:
			g.P("for _, v := range ", accessor, " {")
			g.P("n += ", identEncodeVarint, "(buf[n:],", identZigzagEncode64, "(int64(v)))")
			g.P("}")
		default:
			g.P("for _, v := range ", accessor, " {")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(v))")
			g.P("}")
		}
		g.P("}")

	case f.Wire == core.WireFixed64:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")*8))")
		g.P("for _, v := range ", accessor, " {")
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(v))")
		g.P("n += 8")
		g.P("}")
		g.P("}")

	case f.Wire == core.WireFixed32:
		g.P("if len(", accessor, ") > 0 {")
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")*4))")
		g.P("for _, v := range ", accessor, " {")
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(v))")
		g.P("n += 4")
		g.P("}")
		g.P("}")
	}
}

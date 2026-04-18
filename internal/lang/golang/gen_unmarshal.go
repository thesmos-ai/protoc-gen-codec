// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"fmt"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var identStringsBuilder = protogen.GoIdent{GoName: "Builder", GoImportPath: "strings"}

func emitErrVarint(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrInvalidVarint, ")")
}

func emitErrShort(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrBufferTooShort, ")")
}

func emitErrWireType(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrInvalidWireType, ")")
}

func emitErrFixedLen(g *protogen.GeneratedFile, fieldNum int32) {
	g.P("return ", identFmtErrorf, `("field %d: %w", `, fieldNum, ", ", identErrInvalidLength, ")")
}

type slabMode int

const (
	slabNone slabMode = iota
	slabNaive
	slabSmart
)

func classifySlab(info *core.MessageInfo) slabMode {
	hasStr := false
	hasBytes := false
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsString {
			hasStr = true
		}
		if f.IsBytes {
			hasBytes = true
		}
	}
	if !hasStr {
		return slabNone
	}
	if !hasBytes {
		return slabNaive
	}
	return slabSmart
}

func stringFieldNumbers(info *core.MessageInfo) []int32 {
	var nums []int32
	for i := range info.Fields {
		if info.Fields[i].IsString {
			nums = append(nums, info.Fields[i].ProtoNum)
		}
	}
	return nums
}

// emitWireScan emits a wire-format scan loop. For each string field number
// in strNums, it calls bodyFn with the field data slice expression.
func emitWireScan(g *protogen.GeneratedFile, strNums []int32, iterVar string, bodyFn func(num int32)) {
	g.P(iterVar, " := 0")
	g.P("for ", iterVar, " < l {")
	g.P("stag, sn := ", identDecodeVarint, "(data[", iterVar, ":])")
	g.P("if sn < 0 { break }")
	g.P(iterVar, " += sn")
	g.P("swt := stag & 0x7")
	g.P("sfn := stag >> 3")
	g.P("switch swt {")
	g.P("case 0:")
	g.P("_, sn = ", identDecodeVarint, "(data[", iterVar, ":])")
	g.P("if sn < 0 { break }")
	g.P(iterVar, " += sn")
	g.P("case 1:")
	g.P("if ", iterVar, "+8 > l { break }")
	g.P(iterVar, " += 8")
	g.P("case 2:")
	g.P("svl, sn := ", identDecodeVarint, "(data[", iterVar, ":])")
	g.P("if sn < 0 { break }")
	g.P(iterVar, " += sn")
	g.P("if svl > uint64(l-", iterVar, ") { break }")
	g.P("switch sfn {")
	for _, num := range strNums {
		g.P("case ", num, ":")
		bodyFn(num)
	}
	g.P("}")
	g.P(iterVar, " += int(svl)")
	g.P("case 5:")
	g.P("if ", iterVar, "+4 > l { break }")
	g.P(iterVar, " += 4")
	g.P("}")
	g.P("}")
}

// isPoolableOptional returns true if a field participates in the
// seenOptional-bitmap pointer-pooling path: proto3-optional scalars (*T) and
// singular message-kind pointer fields (*Inner). Repeated and value-inlined
// fields are excluded — their pooling is handled elsewhere (cursor reuse,
// keep_capacity on slices).
func isPoolableOptional(f *core.FieldInfo) bool {
	if f.IsRepeated {
		return false
	}
	if f.IsMessage && f.UsePointer {
		return true
	}
	if f.IsProto3Optional && !f.IsMessage {
		return true
	}
	return false
}

// hasRepeatedMessageField returns true if the message has any repeated
// message-kind field eligible for pre-scan capacity hinting.
func hasRepeatedMessageField(info *core.MessageInfo) bool {
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsMessage && f.IsRepeated {
			return true
		}
	}
	return false
}

func generateUnmarshalCodec(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	mode := classifySlab(info)

	// Detect whether to enable pointer pooling. When enabled, existing *T
	// heap slots on the receiver are reused instead of allocating fresh on
	// every unmarshal. A uint64 bitmap tracks which poolable fields were
	// decoded so absent ones can be nilled out at end-of-method. Field
	// numbers > 63 overflow a single uint64, so those messages fall back to
	// the non-pooled path (unchanged behavior). Covers both proto3 optional
	// scalars and singular nested-message pointers.
	hasPoolableFields := false
	var maxPoolProtoNum int32
	for i := range info.Fields {
		f := &info.Fields[i]
		if isPoolableOptional(f) {
			hasPoolableFields = true
			if f.ProtoNum > maxPoolProtoNum {
				maxPoolProtoNum = f.ProtoNum
			}
		}
	}
	poolingEnabled := hasPoolableFields && maxPoolProtoNum <= 63

	// Pre-scan for repeated-message capacity hints: counts occurrences of
	// each repeated-message field number on the wire in a single pass before
	// the main decode loop. When the target slice is nil, pre-sizes it to
	// the counted length to eliminate append-growth reallocs on cold path.
	// Skipped entirely if the message has no repeated-message fields.
	prescanEnabled := hasRepeatedMessageField(info)

	g.P("func (m *", info.TargetType, ") UnmarshalCodec(data []byte) error {")
	g.P("l := len(data)")
	g.P("i := 0")

	if poolingEnabled {
		g.P("var seenOptional uint64")
	}

	for i := range info.Fields {
		f := &info.Fields[i]
		if poolingEnabled && isPoolableOptional(f) {
			// Pointer pooling: keep existing *T slot so it can be reused.
			// Unseen fields are nilled at end-of-method via seenOptional.
			continue
		}
		generateFieldReset(g, fileMap, f)
	}

	if prescanEnabled {
		generateRepeatedMessagePrescan(g, fileMap, info)
	}

	switch mode {
	case slabNaive:
		g.P("dataStr := string(data)")
	case slabSmart:
		generateSmartSlab(g, info)
	}

	g.P("for i < l {")
	g.P("tag, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	g.P("return ", identFmtErrorf, `("offset %d: %w", i, `, identErrInvalidTag, ")")
	g.P("}")
	g.P("i += n")
	g.P("fieldNum := tag >> 3")
	g.P("wireType := tag & 0x7")
	g.P()
	g.P("switch fieldNum {")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldUnmarshal(g, fileMap, f, mode, poolingEnabled)
	}

	g.P("default:")
	g.P("n, err := ", identSkipField, "(data[i:], wireType)")
	g.P("if err != nil {")
	g.P("return err")
	g.P("}")
	g.P("i += n")
	g.P("}")
	g.P("}")

	if poolingEnabled {
		// Nil out any poolable field whose bit wasn't set in seenOptional.
		// This preserves the "absent field = nil" invariant while enabling
		// pointer reuse when the same field is consistently present.
		for i := range info.Fields {
			f := &info.Fields[i]
			if !isPoolableOptional(f) {
				continue
			}
			g.P("if seenOptional&(1<<", f.ProtoNum, ") == 0 { m.", f.TargetName, " = nil }")
		}
	}

	g.P("return nil")
	g.P("}")
}

// generateRepeatedMessagePrescan emits a single wire-walk that counts the
// occurrences of every repeated-message field, then pre-sizes each target
// slice when it is nil (cold path). Warm-path receivers with non-nil slices
// skip the pre-allocation because cursor-reuse (keep_capacity) or the normal
// append path already has backing storage.
//
// Doubling the wire-walk adds a constant overhead per unmarshal, paid for by
// eliminating append-growth reallocs whenever the typical repeat count is >=2.
func generateRepeatedMessagePrescan(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	var fields []*core.FieldInfo
	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsMessage && f.IsRepeated {
			fields = append(fields, f)
		}
	}
	if len(fields) == 0 {
		return
	}
	g.P("{")
	for _, f := range fields {
		g.P("var preCount_", f.ProtoNum, " int")
	}
	g.P("pi := 0")
	g.P("for pi < l {")
	g.P("ptag, pn := ", identDecodeVarint, "(data[pi:])")
	g.P("if pn < 0 { break }")
	g.P("pi += pn")
	g.P("pwt := ptag & 0x7")
	g.P("pfn := ptag >> 3")
	g.P("switch pwt {")
	g.P("case 0:")
	g.P("_, pn = ", identDecodeVarint, "(data[pi:])")
	g.P("if pn < 0 { break }")
	g.P("pi += pn")
	g.P("case 1:")
	g.P("if pi+8 > l { break }")
	g.P("pi += 8")
	g.P("case 2:")
	g.P("pvl, pn := ", identDecodeVarint, "(data[pi:])")
	g.P("if pn < 0 { break }")
	g.P("pi += pn")
	g.P("if pvl > uint64(l-pi) { break }")
	g.P("switch pfn {")
	for _, f := range fields {
		g.P("case ", f.ProtoNum, ":")
		g.P("preCount_", f.ProtoNum, "++")
	}
	g.P("}")
	g.P("pi += int(pvl)")
	g.P("case 5:")
	g.P("if pi+4 > l { break }")
	g.P("pi += 4")
	g.P("}")
	g.P("}")
	for _, f := range fields {
		elemType := goIdentForMessage(g, fileMap, f)
		g.P("if preCount_", f.ProtoNum, " > 0 && m.", f.TargetName, " == nil {")
		if f.UsePointer {
			g.P("m.", f.TargetName, " = make([]*", elemType, ", 0, preCount_", f.ProtoNum, ")")
		} else {
			g.P("m.", f.TargetName, " = make([]", elemType, ", 0, preCount_", f.ProtoNum, ")")
		}
		g.P("}")
	}
	g.P("}")
}

func generateSmartSlab(g *protogen.GeneratedFile, info *core.MessageInfo) {
	strNums := stringFieldNumbers(info)

	// Pass 1: measure total string bytes
	g.P("strTotal := 0")
	g.P("{")
	emitWireScan(g, strNums, "si", func(_ int32) {
		g.P("strTotal += int(svl)")
	})
	g.P("}")
	g.P()

	// Pass 2: extract string bytes into slab, then seal
	g.P("var slab string")
	g.P("if strTotal > 0 {")
	g.P("var strSlab ", identStringsBuilder)
	g.P("strSlab.Grow(strTotal)")
	g.P("{")
	emitWireScan(g, strNums, "si", func(_ int32) {
		g.P("strSlab.Write(data[si : si+int(svl)])")
	})
	g.P("}")
	g.P("slab = strSlab.String()")
	g.P("}")
	g.P("slabOff := 0")
	g.P()
}

func generateFieldUnmarshal(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, mode slabMode, poolingEnabled bool) {
	accessor := "m." + f.TargetName

	g.P("case ", f.ProtoNum, ":")

	// WKT dispatch must precede IsMessage/IsMap checks; see generateFieldMarshal.
	if f.WellKnown == core.WKTTimestamp || f.WellKnown == core.WKTDuration {
		g.P("if wireType != ", int(core.WireLenDel), " {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		if f.WellKnown == core.WKTTimestamp {
			g.P("wktV, err := ", identDecodeTimestamp, "(data[i:i+int(vLen)])")
		} else {
			g.P("wktV, err := ", identDecodeDuration, "(data[i:i+int(vLen)])")
		}
		g.P("if err != nil {")
		g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
		g.P("}")
		g.P(accessor, " = wktV")
		g.P("i += int(vLen)")
		return
	}

	if f.IsMap {
		generateMapFieldUnmarshal(g, f, accessor)
		return
	}

	if f.IsRepeated {
		generateRepeatedFieldUnmarshal(g, fileMap, f, mode)
		return
	}

	if f.IsMessage {
		g.P("if wireType != ", int(core.WireLenDel), " {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		if f.UsePointer {
			g.P("if ", accessor, " == nil {")
			g.P(accessor, " = new(", goIdentForMessage(g, fileMap, f), ")")
			g.P("}")
			g.P("if err := ", accessor, ".UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
			g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
			g.P("}")
			if poolingEnabled {
				// Mark as seen so the post-loop nil-out pass skips this field.
				// Without pooling, the top-of-method reset already nils it and
				// the branch above re-allocates on every call.
				g.P("seenOptional |= 1 << ", f.ProtoNum)
			}
		} else {
			g.P("if err := (&", accessor, ").UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
			g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
			g.P("}")
		}
		g.P("i += int(vLen)")
		return
	}

	if f.IsProto3Optional {
		generatePresenceFieldUnmarshal(g, fileMap, f, accessor, poolingEnabled)
		return
	}

	g.P("if wireType != ", int(f.Wire), " {")
	emitErrWireType(g, f.ProtoNum)
	g.P("}")

	switch {
	case f.FixedLen > 0:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if vLen != ", f.FixedLen, " {")
		emitErrFixedLen(g, f.ProtoNum)
		g.P("}")
		g.P("if l-i < ", f.FixedLen, " {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("copy(", accessor, "[:], data[i:i+", f.FixedLen, "])")
		g.P("i += ", f.FixedLen)

	case f.Wire == core.WireVarint:
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		switch {
		case f.ProtoKind == protoreflect.BoolKind:
			g.P(accessor, " = v != 0")
		case f.ProtoKind == protoreflect.Sint32Kind:
			g.P(accessor, " = ", castExpr(g, fileMap, f, fmt.Sprintf("%s(uint32(v))", g.QualifiedGoIdent(identZigzagDecode32))))
		case f.ProtoKind == protoreflect.Sint64Kind:
			g.P(accessor, " = ", castExpr(g, fileMap, f, fmt.Sprintf("%s(v)", g.QualifiedGoIdent(identZigzagDecode64))))
		default:
			g.P(accessor, " = ", castExpr(g, fileMap, f, "v"))
		}

	case f.Wire == core.WireFixed64:
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint64(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P(accessor, " = ", castExpr64(g, fileMap, f, readExpr))
		g.P("i += 8")

	case f.Wire == core.WireFixed32:
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint32(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P(accessor, " = ", castExpr32(g, fileMap, f, readExpr))
		g.P("i += 4")

	case f.IsString:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		switch mode {
		case slabNaive:
			g.P(accessor, " = dataStr[i : i+int(vLen)]")
		case slabSmart:
			g.P("se := slabOff + int(vLen)")
			g.P("if se > len(slab) { se = len(slab) }")
			g.P(accessor, " = slab[slabOff:se]")
			g.P("slabOff = se")
		default:
			g.P(accessor, " = string(data[i : i+int(vLen)])")
		}
		g.P("i += int(vLen)")

	case f.IsBytes:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P(accessor, " = append(", accessor, "[:0], data[i:i+int(vLen)]...)")
		g.P("i += int(vLen)")
	}
}

// generateMapFieldUnmarshal emits the decode path for a proto3 map field.
// On the wire, each entry is a length-delimited record containing a
// key (field 1) and value (field 2) pair. The Go target is map[K]V.
func generateMapFieldUnmarshal(g *protogen.GeneratedFile, f *core.FieldInfo, accessor string) {
	g.P("if wireType != ", int(core.WireLenDel), " {")
	emitErrWireType(g, f.ProtoNum)
	g.P("}")
	g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
	g.P("if n < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += n")
	g.P("if uint64(l-i) < vLen {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	keyType := mapKeyGoType(f)
	valType := mapValueGoType(f)
	g.P("if ", accessor, " == nil {")
	g.P(accessor, " = make(map[", keyType, "]", valType, ")")
	g.P("}")
	g.P("entryEnd := i + int(vLen)")
	g.P("var mk ", keyType)
	g.P("var mv ", valType)
	g.P("for i < entryEnd {")
	g.P("etag, en := ", identDecodeVarint, "(data[i:entryEnd])")
	g.P("if en < 0 {")
	emitErrVarint(g, f.ProtoNum)
	g.P("}")
	g.P("i += en")
	g.P("switch etag >> 3 {")
	g.P("case ", f.MapKey.ProtoNum, ":")
	emitScalarRead(g, f.MapKey, "mk")
	g.P("case ", f.MapValue.ProtoNum, ":")
	emitScalarRead(g, f.MapValue, "mv")
	g.P("default:")
	g.P("sn, err := ", identSkipField, "(data[i:entryEnd], etag & 0x7)")
	g.P("if err != nil { return err }")
	g.P("i += sn")
	g.P("}")
	g.P("}")
	g.P("if i != entryEnd {")
	emitErrShort(g, f.ProtoNum)
	g.P("}")
	g.P(accessor, "[mk] = mv")
}

// generatePresenceFieldUnmarshal emits the decode path for a proto3 optional
// scalar field modeled as a Go pointer (e.g. optional int32 -> *int32).
//
// When poolingEnabled is false (the fallback path, used when any optional
// scalar has proto num > 63), it decodes into a local `tmp` then assigns
// &tmp to the field — always allocating fresh.
//
// When poolingEnabled is true, the existing *T slot is reused if non-nil
// (zero allocs on warm path); a uint64 `seenOptional` bitmap tracks which
// fields were decoded so absent ones are nilled at end-of-method.
func generatePresenceFieldUnmarshal(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, accessor string, poolingEnabled bool) {
	g.P("if wireType != ", int(f.Wire), " {")
	emitErrWireType(g, f.ProtoNum)
	g.P("}")

	var rhs string
	switch f.Wire {
	case core.WireVarint:
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		switch f.ProtoKind {
		case protoreflect.BoolKind:
			rhs = "v != 0"
		case protoreflect.Sint32Kind:
			rhs = castExpr(g, fileMap, f, fmt.Sprintf("%s(uint32(v))", g.QualifiedGoIdent(identZigzagDecode32)))
		case protoreflect.Sint64Kind:
			rhs = castExpr(g, fileMap, f, fmt.Sprintf("%s(v)", g.QualifiedGoIdent(identZigzagDecode64)))
		default:
			rhs = castExpr(g, fileMap, f, "v")
		}
	case core.WireFixed64:
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint64(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		rhs = castExpr64(g, fileMap, f, readExpr)
	case core.WireFixed32:
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint32(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		rhs = castExpr32(g, fileMap, f, readExpr)
	}

	if poolingEnabled {
		g.P("if ", accessor, " == nil {")
		g.P(accessor, " = new(", elemGoType(g, fileMap, f), ")")
		g.P("}")
		g.P("*", accessor, " = ", rhs)
		g.P("seenOptional |= 1 << ", f.ProtoNum)
	} else {
		g.P("tmp := ", rhs)
		g.P(accessor, " = &tmp")
	}

	// Trailing index bump for fixed-width wires (varint already advanced).
	switch f.Wire {
	case core.WireFixed64:
		g.P("i += 8")
	case core.WireFixed32:
		g.P("i += 4")
	}
}

func generateRepeatedFieldUnmarshal(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, mode slabMode) {
	accessor := "m." + f.TargetName

	if f.IsMessage {
		g.P("if wireType != ", int(core.WireLenDel), " {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		elemType := goIdentForMessage(g, fileMap, f)
		if f.UsePointer {
			if f.KeepCapacity {
				// Cursor-reuse path: when keep_capacity leaves the backing
				// array intact across resets, reuse any existing *T slot in
				// the spare capacity instead of allocating a fresh one.
				g.P("var elem *", elemType)
				g.P("if len(", accessor, ") < cap(", accessor, ") {")
				g.P(accessor, " = ", accessor, "[:len(", accessor, ")+1]")
				g.P("elem = ", accessor, "[len(", accessor, ")-1]")
				g.P("if elem == nil {")
				g.P("elem = new(", elemType, ")")
				g.P(accessor, "[len(", accessor, ")-1] = elem")
				g.P("}")
				g.P("} else {")
				g.P("elem = new(", elemType, ")")
				g.P(accessor, " = append(", accessor, ", elem)")
				g.P("}")
				g.P("if err := elem.UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
				g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
				g.P("}")
			} else {
				g.P("elem := new(", elemType, ")")
				g.P("if err := elem.UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
				g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
				g.P("}")
				g.P(accessor, " = append(", accessor, ", elem)")
			}
		} else {
			// Zero-alloc: append a zero value, then decode into the last element.
			g.P(accessor, " = append(", accessor, ", ", elemType, "{})")
			g.P("if err := ", accessor, "[len(", accessor, ")-1].UnmarshalCodec(data[i:i+int(vLen)]); err != nil {")
			g.P("return ", identFmtErrorf, `("field %d: %w", `, f.ProtoNum, ", err)")
			g.P("}")
		}
		g.P("i += int(vLen)")
		return
	}

	switch {
	case f.IsString:
		g.P("if wireType != 2 {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		switch mode {
		case slabNaive:
			g.P(accessor, " = append(", accessor, ", dataStr[i:i+int(vLen)])")
		case slabSmart:
			g.P("se := slabOff + int(vLen)")
			g.P("if se > len(slab) { se = len(slab) }")
			g.P(accessor, " = append(", accessor, ", slab[slabOff:se])")
			g.P("slabOff = se")
		default:
			g.P(accessor, " = append(", accessor, ", string(data[i:i+int(vLen)]))")
		}
		g.P("i += int(vLen)")

	case f.IsBytes && f.FixedLen > 0:
		zeroType := goCastName(g, fileMap, f)
		g.P("if wireType != 2 {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if vLen != ", f.FixedLen, " {")
		emitErrFixedLen(g, f.ProtoNum)
		g.P("}")
		g.P("if l-i < ", f.FixedLen, " {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		if zeroType != "" {
			g.P("var elem ", zeroType)
		} else {
			g.P("var elem [", f.FixedLen, "]byte")
		}
		g.P("copy(elem[:], data[i:i+", f.FixedLen, "])")
		g.P(accessor, " = append(", accessor, ", elem)")
		g.P("i += ", f.FixedLen)

	case f.IsBytes:
		g.P("if wireType != 2 {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if uint64(l-i) < vLen {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("elem := make([]byte, vLen)")
		g.P("copy(elem, data[i:i+int(vLen)])")
		g.P(accessor, " = append(", accessor, ", elem)")
		g.P("i += int(vLen)")

	case f.Wire == core.WireVarint:
		var elemExpr string
		switch f.ProtoKind {
		case protoreflect.Sint32Kind:
			elemExpr = castExpr(g, fileMap, f, fmt.Sprintf("%s(uint32(v))", g.QualifiedGoIdent(identZigzagDecode32)))
		case protoreflect.Sint64Kind:
			elemExpr = castExpr(g, fileMap, f, fmt.Sprintf("%s(v)", g.QualifiedGoIdent(identZigzagDecode64)))
		default:
			elemExpr = castExpr(g, fileMap, f, "v")
		}
		g.P("if wireType == 2 {")
		g.P("pLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("end := i + int(pLen)")
		g.P("if end > l {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("if ", accessor, " == nil {")
		g.P(accessor, " = make([]", elemGoType(g, fileMap, f), ", 0, int(pLen))")
		g.P("}")
		g.P("for i < end {")
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P(accessor, " = append(", accessor, ", ", elemExpr, ")")
		g.P("}")
		g.P("} else if wireType == 0 {")
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P(accessor, " = append(", accessor, ", ", elemExpr, ")")
		g.P("} else {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")

	case f.Wire == core.WireFixed64:
		readExpr := fmt.Sprintf("%s.Uint64(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P("if wireType == 2 {")
		g.P("pLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("end := i + int(pLen)")
		g.P("if end > l {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("if ", accessor, " == nil {")
		g.P(accessor, " = make([]", elemGoType(g, fileMap, f), ", 0, int(pLen)/8)")
		g.P("}")
		g.P("for i+8 <= end {")
		g.P(accessor, " = append(", accessor, ", ", castExpr64(g, fileMap, f, readExpr), ")")
		g.P("i += 8")
		g.P("}")
		g.P("} else if wireType == 1 {")
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P(accessor, " = append(", accessor, ", ", castExpr64(g, fileMap, f, readExpr), ")")
		g.P("i += 8")
		g.P("} else {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")

	case f.Wire == core.WireFixed32:
		readExpr := fmt.Sprintf("%s.Uint32(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P("if wireType == 2 {")
		g.P("pLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("end := i + int(pLen)")
		g.P("if end > l {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P("if ", accessor, " == nil {")
		g.P(accessor, " = make([]", elemGoType(g, fileMap, f), ", 0, int(pLen)/4)")
		g.P("}")
		g.P("for i+4 <= end {")
		g.P(accessor, " = append(", accessor, ", ", castExpr32(g, fileMap, f, readExpr), ")")
		g.P("i += 4")
		g.P("}")
		g.P("} else if wireType == 5 {")
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P(accessor, " = append(", accessor, ", ", castExpr32(g, fileMap, f, readExpr), ")")
		g.P("i += 4")
		g.P("} else {")
		emitErrWireType(g, f.ProtoNum)
		g.P("}")
	}
}

func castExpr(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, varName string) string {
	if name := goCastName(g, fileMap, f); name != "" {
		return name + "(" + varName + ")"
	}
	return defaultCast(f.ProtoKind, varName)
}

func castExpr64(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, readExpr string) string {
	if name := goCastName(g, fileMap, f); name != "" {
		return name + "(" + readExpr + ")"
	}
	switch f.ProtoKind {
	case protoreflect.Sfixed64Kind:
		return fmt.Sprintf("int64(%s)", readExpr)
	default:
		return readExpr
	}
}

func castExpr32(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo, readExpr string) string {
	if name := goCastName(g, fileMap, f); name != "" {
		return name + "(" + readExpr + ")"
	}
	switch f.ProtoKind {
	case protoreflect.Sfixed32Kind:
		return fmt.Sprintf("int32(%s)", readExpr)
	default:
		return readExpr
	}
}

func defaultCast(k protoreflect.Kind, varName string) string {
	switch k {
	case protoreflect.Int32Kind:
		return fmt.Sprintf("int32(%s)", varName)
	// Sint{32,64}Kind callers apply ZigzagDecode at the callsite; no outer cast here.
	case protoreflect.Sint32Kind:
		return varName
	case protoreflect.Uint32Kind, protoreflect.EnumKind:
		return fmt.Sprintf("uint32(%s)", varName)
	case protoreflect.Int64Kind:
		return fmt.Sprintf("int64(%s)", varName)
	// Sint{32,64}Kind callers apply ZigzagDecode at the callsite; no outer cast here.
	case protoreflect.Sint64Kind:
		return varName
	case protoreflect.Uint64Kind:
		return varName
	case protoreflect.BoolKind:
		return fmt.Sprintf("%s != 0", varName)
	default:
		return varName
	}
}

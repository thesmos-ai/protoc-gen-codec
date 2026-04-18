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

func generateUnmarshalCodec(g *protogen.GeneratedFile, info *core.MessageInfo) {
	mode := classifySlab(info)

	g.P("func (m *", info.GoType, ") UnmarshalCodec(data []byte) error {")
	g.P("l := len(data)")
	g.P("i := 0")

	for i := range info.Fields {
		f := &info.Fields[i]
		if f.IsRepeated {
			g.P("m.", f.GoName, " = m.", f.GoName, "[:0]")
		} else if f.IsBytes && f.FixedLen == 0 {
			g.P("m.", f.GoName, " = m.", f.GoName, "[:0]")
		}
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
		generateFieldUnmarshal(g, f, mode)
	}

	g.P("default:")
	g.P("n, err := ", identSkipField, "(data[i:], wireType)")
	g.P("if err != nil {")
	g.P("return err")
	g.P("}")
	g.P("i += n")
	g.P("}")
	g.P("}")

	g.P("return nil")
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

func generateFieldUnmarshal(g *protogen.GeneratedFile, f *core.FieldInfo, mode slabMode) {
	accessor := "m." + f.GoName

	g.P("case ", f.ProtoNum, ":")

	if f.IsRepeated {
		generateRepeatedFieldUnmarshal(g, f, mode)
		return
	}

	g.P("if wireType != ", int(f.Wire), " {")
	g.P("return ", identErrInvalidWireType)
	g.P("}")

	switch {
	case f.FixedLen > 0:
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if vLen != ", f.FixedLen, " {")
		g.P("return ", identErrInvalidLength)
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
		if f.ProtoKind == protoreflect.BoolKind {
			g.P(accessor, " = v != 0")
		} else {
			g.P(accessor, " = ", castExpr(g, f, "v"))
		}

	case f.Wire == core.WireFixed64:
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint64(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P(accessor, " = ", castExpr64(g, f, readExpr))
		g.P("i += 8")

	case f.Wire == core.WireFixed32:
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		readExpr := fmt.Sprintf("%s.Uint32(data[i:])", g.QualifiedGoIdent(identBinaryLE))
		g.P(accessor, " = ", castExpr32(g, f, readExpr))
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

func generateRepeatedFieldUnmarshal(g *protogen.GeneratedFile, f *core.FieldInfo, mode slabMode) {
	accessor := "m." + f.GoName

	switch {
	case f.IsString:
		g.P("if wireType != 2 {")
		g.P("return ", identErrInvalidWireType)
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
		zeroType := f.QualifiedZeroType(g)
		g.P("if wireType != 2 {")
		g.P("return ", identErrInvalidWireType)
		g.P("}")
		g.P("vLen, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P("if vLen != ", f.FixedLen, " {")
		g.P("return ", identErrInvalidLength)
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
		g.P("return ", identErrInvalidWireType)
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
		g.P("for i < end {")
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P(accessor, " = append(", accessor, ", ", castExpr(g, f, "v"), ")")
		g.P("}")
		g.P("} else if wireType == 0 {")
		g.P("v, n := ", identDecodeVarint, "(data[i:])")
		g.P("if n < 0 {")
		emitErrVarint(g, f.ProtoNum)
		g.P("}")
		g.P("i += n")
		g.P(accessor, " = append(", accessor, ", ", castExpr(g, f, "v"), ")")
		g.P("} else {")
		g.P("return ", identErrInvalidWireType)
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
		g.P("for i+8 <= end {")
		g.P(accessor, " = append(", accessor, ", ", castExpr64(g, f, readExpr), ")")
		g.P("i += 8")
		g.P("}")
		g.P("} else if wireType == 1 {")
		g.P("if l-i < 8 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P(accessor, " = append(", accessor, ", ", castExpr64(g, f, readExpr), ")")
		g.P("i += 8")
		g.P("} else {")
		g.P("return ", identErrInvalidWireType)
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
		g.P("for i+4 <= end {")
		g.P(accessor, " = append(", accessor, ", ", castExpr32(g, f, readExpr), ")")
		g.P("i += 4")
		g.P("}")
		g.P("} else if wireType == 5 {")
		g.P("if l-i < 4 {")
		emitErrShort(g, f.ProtoNum)
		g.P("}")
		g.P(accessor, " = append(", accessor, ", ", castExpr32(g, f, readExpr), ")")
		g.P("i += 4")
		g.P("} else {")
		g.P("return ", identErrInvalidWireType)
		g.P("}")
	}
}

func castExpr(g *protogen.GeneratedFile, f *core.FieldInfo, varName string) string {
	if f.CastIdent != nil {
		return fmt.Sprintf("%s(%s)", g.QualifiedGoIdent(*f.CastIdent), varName)
	}
	if f.CastLocal != "" {
		return fmt.Sprintf("%s(%s)", f.CastLocal, varName)
	}
	return defaultCast(f.ProtoKind, varName)
}

func castExpr64(g *protogen.GeneratedFile, f *core.FieldInfo, readExpr string) string {
	if f.CastIdent != nil {
		return fmt.Sprintf("%s(%s)", g.QualifiedGoIdent(*f.CastIdent), readExpr)
	}
	if f.CastLocal != "" {
		return fmt.Sprintf("%s(%s)", f.CastLocal, readExpr)
	}
	switch f.ProtoKind {
	case protoreflect.Sfixed64Kind:
		return fmt.Sprintf("int64(%s)", readExpr)
	default:
		return readExpr
	}
}

func castExpr32(g *protogen.GeneratedFile, f *core.FieldInfo, readExpr string) string {
	if f.CastIdent != nil {
		return fmt.Sprintf("%s(%s)", g.QualifiedGoIdent(*f.CastIdent), readExpr)
	}
	if f.CastLocal != "" {
		return fmt.Sprintf("%s(%s)", f.CastLocal, readExpr)
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
	case protoreflect.Int32Kind, protoreflect.Sint32Kind:
		return fmt.Sprintf("int32(%s)", varName)
	case protoreflect.Uint32Kind, protoreflect.EnumKind:
		return fmt.Sprintf("uint32(%s)", varName)
	case protoreflect.Int64Kind, protoreflect.Sint64Kind:
		return fmt.Sprintf("int64(%s)", varName)
	case protoreflect.Uint64Kind:
		return varName
	case protoreflect.BoolKind:
		return fmt.Sprintf("%s != 0", varName)
	default:
		return varName
	}
}

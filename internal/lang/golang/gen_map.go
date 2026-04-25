// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.thesmos.sh/protoc-gen-codec/internal/core"
)

// scalarSizeExpr returns a Go expression computing the wire size of a
// singular scalar field (excluding its tag). Used for synthetic map entry
// keys and values. The expression references codec.Sov via the generated
// file's import alias.
func scalarSizeExpr(g *protogen.GeneratedFile, f *core.FieldInfo, v string) string {
	sov := g.QualifiedGoIdent(identSizeVarint)
	switch {
	case f.IsString, f.IsBytes:
		return fmt.Sprintf("%s(uint64(len(%s))) + len(%s)", sov, v, v)
	case f.Wire == core.WireFixed64:
		return "8"
	case f.Wire == core.WireFixed32:
		return "4"
	case f.ProtoKind == protoreflect.BoolKind:
		return "1"
	default:
		return fmt.Sprintf("%s(uint64(%s))", sov, v)
	}
}

// emitScalarWrite emits code that writes a tag+value for a synthetic
// map entry sub-field into buf[n:].
func emitScalarWrite(g *protogen.GeneratedFile, f *core.FieldInfo, v string) {
	emitTag(g, f.ProtoNum, f.Wire)
	switch {
	case f.IsString:
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", v, ")))")
		g.P("n += copy(buf[n:], ", v, ")")
	case f.IsBytes:
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", v, ")))")
		g.P("n += copy(buf[n:], ", v, ")")
	case f.Wire == core.WireFixed64:
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", v, "))")
		g.P("n += 8")
	case f.Wire == core.WireFixed32:
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", v, "))")
		g.P("n += 4")
	default:
		if f.ProtoKind == protoreflect.BoolKind {
			// When v is a Go boolean literal (the bool-keyed map case
			// emits `false` and `true` directly), inline the byte so
			// coverage doesn't see an always-false branch on the
			// always-true side and vice versa.
			switch v {
			case "true":
				g.P("buf[n] = 1")
			case "false":
				g.P("buf[n] = 0")
			default:
				g.P("if ", v, " { buf[n] = 1 } else { buf[n] = 0 }")
			}
			g.P("n++")
		} else {
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", v, "))")
		}
	}
}

// emitScalarRead emits code that decodes a single scalar from the map
// entry slice data[i:entryEnd] into dst (a pre-declared variable of the
// correct Go type). All reads are bounded by entryEnd so a malformed
// entry cannot cause out-of-range access. String reads borrow from the
// shared top-level `slab` so map-entry strings participate in the
// cross-message slab allocation; `i` advances through the outer buffer,
// so slab[slabOff+i : ...] is the correct absolute index.
func emitScalarRead(g *protogen.GeneratedFile, f *core.FieldInfo, dst string) {
	goType := scalarGoType(f)
	switch {
	case f.IsString:
		g.P("sVLen, sN := ", identDecodeVarint, "(data[i:entryEnd])")
		g.P("if sN < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += sN")
		g.P("if sVLen > uint64(entryEnd-i) { return ", identErrBufferTooShort, " }")
		g.P(dst, " = slab[slabOff+i : slabOff+i+int(sVLen)]")
		g.P("i += int(sVLen)")
	case f.IsBytes:
		g.P("sVLen, sN := ", identDecodeVarint, "(data[i:entryEnd])")
		g.P("if sN < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += sN")
		g.P("if sVLen > uint64(entryEnd-i) { return ", identErrBufferTooShort, " }")
		g.P(dst, " = append([]byte(nil), data[i:i+int(sVLen)]...)")
		g.P("i += int(sVLen)")
	case f.Wire == core.WireFixed64:
		g.P("if entryEnd-i < 8 { return ", identErrBufferTooShort, " }")
		g.P(dst, " = ", goType, "(", identBinaryLE, ".Uint64(data[i:]))")
		g.P("i += 8")
	case f.Wire == core.WireFixed32:
		g.P("if entryEnd-i < 4 { return ", identErrBufferTooShort, " }")
		g.P(dst, " = ", goType, "(", identBinaryLE, ".Uint32(data[i:]))")
		g.P("i += 4")
	default:
		g.P("sV, sN := ", identDecodeVarint, "(data[i:entryEnd])")
		g.P("if sN < 0 { return ", identErrInvalidVarint, " }")
		g.P("i += sN")
		if f.ProtoKind == protoreflect.BoolKind {
			g.P(dst, " = sV != 0")
		} else {
			g.P(dst, " = ", goType, "(sV)")
		}
	}
}

// mapKeyGoType returns the Go type for a map's key field.
func mapKeyGoType(f *core.FieldInfo) string {
	return scalarGoType(f.MapKey)
}

// mapValueGoType returns the Go type for a map's value field.
func mapValueGoType(f *core.FieldInfo) string {
	return scalarGoType(f.MapValue)
}

func scalarGoType(f *core.FieldInfo) string {
	if f.IsString {
		return "string"
	}
	if f.IsBytes {
		return "[]byte"
	}
	switch f.ProtoKind {
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return "int32"
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return "uint32"
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return "int64"
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return "uint64"
	case protoreflect.FloatKind:
		return "float32"
	case protoreflect.DoubleKind:
		return "float64"
	case protoreflect.EnumKind:
		return "uint32"
	case protoreflect.BoolKind:
		return "bool"
	default:
		return "uint64"
	}
}

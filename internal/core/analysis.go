// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"fmt"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type WireKind int

const (
	WireVarint  WireKind = 0
	WireFixed64 WireKind = 1
	WireLenDel  WireKind = 2
	WireFixed32 WireKind = 5
)

type FieldInfo struct {
	ProtoNum     int32
	GoName       string
	Wire         WireKind
	ProtoKind    protoreflect.Kind
	Cast         string
	CastIdent    *protogen.GoIdent
	CastLocal    string
	FixedLen     uint32
	KeepCapacity bool
	IsRepeated   bool
	IsBytes      bool
	IsString     bool
}

type MessageInfo struct {
	GoType string
	Fields []FieldInfo
}

func AnalyzeMessage(
	msg *protogen.Message,
	fileMap map[string]*protogen.File,
	file *protogen.File,
) (*MessageInfo, error) {
	goType := messageGoType(msg)
	if goType == "" {
		return nil, nil
	}

	info := &MessageInfo{GoType: goType}

	for _, field := range msg.Fields {
		fi, err := analyzeField(field, fileMap, file)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Desc.Name(), err)
		}
		info.Fields = append(info.Fields, fi)
	}

	return info, nil
}

func analyzeField(
	field *protogen.Field,
	fileMap map[string]*protogen.File,
	file *protogen.File,
) (FieldInfo, error) {
	fi := FieldInfo{
		ProtoNum:     int32(field.Desc.Number()),
		GoName:       resolveGoName(field),
		Wire:         WireKindOf(field.Desc.Kind()),
		ProtoKind:    field.Desc.Kind(),
		FixedLen:     fieldFixedLen(field),
		KeepCapacity: fieldKeepCapacity(field),
		IsRepeated:   field.Desc.IsList(),
		IsBytes:      field.Desc.Kind() == protoreflect.BytesKind,
		IsString:     field.Desc.Kind() == protoreflect.StringKind,
	}

	cast := fieldGoCast(field)
	fi.Cast = cast
	if cast != "" {
		local, ident := resolveCast(fileMap, file, cast)
		fi.CastLocal = local
		fi.CastIdent = ident
	}

	return fi, nil
}

func resolveGoName(field *protogen.Field) string {
	name := fieldGoField(field)
	if name != "" {
		return name
	}
	return field.GoName
}

func resolveCast(
	fileMap map[string]*protogen.File,
	file *protogen.File,
	cast string,
) (string, *protogen.GoIdent) {
	dotIdx := strings.IndexByte(cast, '.')
	if dotIdx < 0 {
		return cast, nil
	}

	pkgAlias := cast[:dotIdx]
	typeName := cast[dotIdx+1:]

	for _, dep := range file.Proto.GetDependency() {
		depFile, ok := fileMap[dep]
		if !ok {
			continue
		}
		if string(depFile.GoPackageName) == pkgAlias {
			ident := protogen.GoIdent{
				GoName:       typeName,
				GoImportPath: depFile.GoImportPath,
			}
			return "", &ident
		}
	}

	return cast, nil
}

func WireKindOf(k protoreflect.Kind) WireKind {
	switch k {
	case protoreflect.BoolKind, protoreflect.EnumKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind:
		return WireVarint
	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return WireFixed64
	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind, protoreflect.FloatKind:
		return WireFixed32
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
		return WireLenDel
	default:
		return WireVarint
	}
}

func TagValue(fieldNum int32, wk WireKind) uint64 {
	return uint64(fieldNum)<<3 | uint64(wk)
}

func TagSize(fieldNum int32) int {
	v := uint64(fieldNum) << 3
	n := 1
	for v >= 0x80 {
		n++
		v >>= 7
	}
	return n
}

func TagBytes(fieldNum int32, wk WireKind) []byte {
	v := TagValue(fieldNum, wk)
	var buf [10]byte
	i := 0
	for v >= 0x80 {
		buf[i] = byte(v) | 0x80
		v >>= 7
		i++
	}
	buf[i] = byte(v)
	return buf[:i+1]
}

func (f *FieldInfo) QualifiedZeroType(g *protogen.GeneratedFile) string {
	if f.CastIdent != nil {
		return g.QualifiedGoIdent(*f.CastIdent)
	}
	if f.CastLocal != "" {
		return f.CastLocal
	}
	return ""
}

func SovLocal(x uint64) int {
	n := 1
	for x >= 0x80 {
		n++
		x >>= 7
	}
	return n
}

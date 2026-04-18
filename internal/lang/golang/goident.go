// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

// goCastName returns the qualified Go name for the field's cast target,
// or "" if the field has no cast.
func goCastName(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.CastRef == nil {
		return ""
	}
	if f.CastRef.ProtoFile == "" {
		return f.CastRef.Name
	}
	depFile, ok := fileMap[f.CastRef.ProtoFile]
	if !ok {
		return f.CastRef.PackageAlias + "." + f.CastRef.Name
	}
	return g.QualifiedGoIdent(protogen.GoIdent{GoName: f.CastRef.Name, GoImportPath: depFile.GoImportPath})
}

// goIdentForMessage returns the qualified Go type name for a message-kind
// field's target type, including package qualification if the message lives
// in another file.
func goIdentForMessage(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.MessageRef == nil {
		return ""
	}
	if f.MessageRef.ProtoFile == "" {
		return f.MessageRef.TargetType
	}
	depFile, ok := fileMap[f.MessageRef.ProtoFile]
	if !ok {
		return f.MessageRef.TargetType
	}
	return g.QualifiedGoIdent(protogen.GoIdent{
		GoName:       f.MessageRef.TargetType,
		GoImportPath: depFile.GoImportPath,
	})
}

// elemGoType returns the Go element type for a repeated field, used when
// pre-allocating slices in packed-unmarshal paths.
func elemGoType(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.CastRef != nil {
		return goCastName(g, fileMap, f)
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
		return ""
	}
}

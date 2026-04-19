// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"

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
// pre-allocating slices in packed-unmarshal paths. Delegates to scalarGoType
// for the kind → Go type mapping so the single switch table lives there.
func elemGoType(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) string {
	if f.CastRef != nil {
		return goCastName(g, fileMap, f)
	}
	return scalarGoType(f)
}

// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

type fieldFixture struct {
	name     string
	num      int32
	kind     descriptorpb.FieldDescriptorProto_Type
	label    descriptorpb.FieldDescriptorProto_Label
	typeName string
	options  *uninterpretedOptions
}

type uninterpretedOptions struct {
	codecField    string
	codecCast     string
	codecFixedLen uint32
	hasFixedLen   bool
	codecKeepCap  bool
	hasKeepCap    bool
}

func buildField(f fieldFixture) *descriptorpb.FieldDescriptorProto {
	fd := &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(f.name),
		Number: proto.Int32(f.num),
		Type:   f.kind.Enum(),
		Label:  f.label.Enum(),
	}
	if f.typeName != "" {
		fd.TypeName = proto.String(f.typeName)
	}
	if f.options != nil {
		fd.Options = encodeFieldOptions(f.options)
	}
	return fd
}

func encodeFieldOptions(o *uninterpretedOptions) *descriptorpb.FieldOptions {
	opts := &descriptorpb.FieldOptions{}
	var raw []byte
	if o.codecField != "" {
		raw = appendString(raw, int32(optGoField), o.codecField)
	}
	if o.codecCast != "" {
		raw = appendString(raw, int32(optGoCast), o.codecCast)
	}
	if o.hasFixedLen {
		raw = appendVarint(raw, int32(optFixedLen), uint64(o.codecFixedLen))
	}
	if o.hasKeepCap {
		v := uint64(0)
		if o.codecKeepCap {
			v = 1
		}
		raw = appendVarint(raw, int32(optKeepCap), v)
	}
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

func encodeMessageOptions(goType string) *descriptorpb.MessageOptions {
	opts := &descriptorpb.MessageOptions{}
	raw := appendString(nil, int32(optGoType), goType)
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

func appendString(raw []byte, fieldNum int32, val string) []byte {
	tag := uint64(fieldNum)<<3 | 2
	raw = appendUvarint(raw, tag)
	raw = appendUvarint(raw, uint64(len(val)))
	return append(raw, val...)
}

func appendVarint(raw []byte, fieldNum int32, val uint64) []byte {
	// Wire type 0 (varint); tag = (fieldNum << 3) | 0.
	tag := uint64(fieldNum) << 3
	raw = appendUvarint(raw, tag)
	return appendUvarint(raw, val)
}

func appendUvarint(raw []byte, v uint64) []byte {
	for v >= 0x80 {
		raw = append(raw, byte(v)|0x80)
		v >>= 7
	}
	return append(raw, byte(v))
}

// runAnalyzeMessage compiles a synthetic file with a single message M
// (and optional sibling message used as typeName target) and calls
// AnalyzeMessage on M. withMessageOpts controls whether M has (codec.type) set.
func runAnalyzeMessage(t *testing.T, withMessageOpts bool, fields ...fieldFixture) (*MessageInfo, error) {
	t.Helper()
	var fds []*descriptorpb.FieldDescriptorProto
	for _, f := range fields {
		fds = append(fds, buildField(f))
	}
	msgOpts := (*descriptorpb.MessageOptions)(nil)
	if withMessageOpts {
		msgOpts = encodeMessageOptions("M")
	}
	innerName := "Inner"
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("t.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("t"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("M"),
				Field:   fds,
				Options: msgOpts,
			},
			{
				Name: &innerName,
				// Inner has its own codec.type so message-kind fields can resolve cleanly.
				Options: encodeMessageOptions("Inner"),
			},
		},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"t.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}
	file := plugin.Files[0]
	fileMap := map[string]*protogen.File{file.Proto.GetName(): file}
	aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }
	return AnalyzeMessage(file.Messages[0], fileMap, file, aliasOf)
}

// runAnalyzeField is a convenience wrapper for the common case of
// "one field in an M message with (codec.type)=M".
func runAnalyzeField(t *testing.T, f fieldFixture) (*MessageInfo, error) {
	return runAnalyzeMessage(t, true, f)
}

// Pre-built fixtures used by tests for Tasks 2.2-2.5.
var (
	fixedLenZeroFixture = fieldFixture{
		name: "ref", num: 1,
		kind:    descriptorpb.FieldDescriptorProto_TYPE_BYTES,
		label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{hasFixedLen: true, codecFixedLen: 0},
	}
	fixedLenOnStringFixture = fieldFixture{
		name: "id", num: 1,
		kind:    descriptorpb.FieldDescriptorProto_TYPE_STRING,
		label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{hasFixedLen: true, codecFixedLen: 32},
	}
	unresolvedCastFixture = fieldFixture{
		name: "x", num: 1,
		kind:    descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{codecCast: "unknown.Type"},
	}
	castOnMessageFixture = fieldFixture{
		name: "m", num: 1,
		kind:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		typeName: ".t.Inner",
		options:  &uninterpretedOptions{codecCast: "Foo"},
	}
)

// runAnalyzeMessageWithOneof builds a synthetic message M with two fields
// inside a non-synthetic oneof "value" and runs AnalyzeMessage on it.
func runAnalyzeMessageWithOneof(t *testing.T) (*MessageInfo, error) {
	t.Helper()
	oneofName := "value"
	oneofIdx := int32(0)
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("t.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("t"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("M"),
				Options: encodeMessageOptions("M"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: &oneofName},
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name: proto.String("text_val"), Number: proto.Int32(1),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						OneofIndex: &oneofIdx,
					},
					{
						Name: proto.String("int_val"), Number: proto.Int32(2),
						Type:       descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
						Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						OneofIndex: &oneofIdx,
					},
				},
			},
		},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"t.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{fd},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatal(err)
	}
	file := plugin.Files[0]
	fileMap := map[string]*protogen.File{file.Proto.GetName(): file}
	aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }
	return AnalyzeMessage(file.Messages[0], fileMap, file, aliasOf)
}

// runAnalyzeMessageWithSyntheticOneof builds a message with a proto3
// optional int32 field (which descriptor tooling emits as a synthetic
// oneof wrapping that single field) and runs AnalyzeMessage on it.
func runAnalyzeMessageWithSyntheticOneof(t *testing.T) (*MessageInfo, error) {
	t.Helper()
	oneofName := "_x"
	oneofIdx := int32(0)
	proto3Optional := true
	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("t.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("t"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("M"),
				Options: encodeMessageOptions("M"),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: &oneofName},
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					{
						Name: proto.String("x"), Number: proto.Int32(1),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						OneofIndex:     &oneofIdx,
						Proto3Optional: &proto3Optional,
					},
				},
			},
		},
	}
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate:  []string{"t.proto"},
		ProtoFile:       []*descriptorpb.FileDescriptorProto{fd},
		CompilerVersion: &pluginpb.Version{Major: proto.Int32(3), Minor: proto.Int32(12)},
	}
	// proto3 optional requires a plugin-side flag
	opts := protogen.Options{}
	plugin, err := opts.New(req)
	if err != nil {
		t.Fatal(err)
	}
	plugin.SupportedFeatures = uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)
	file := plugin.Files[0]
	fileMap := map[string]*protogen.File{file.Proto.GetName(): file}
	aliasOf := func(dep *protogen.File) string { return string(dep.GoPackageName) }
	return AnalyzeMessage(file.Messages[0], fileMap, file, aliasOf)
}

func invalidCastIdentFixture(cast string) fieldFixture {
	return fieldFixture{
		name: "x", num: 1,
		kind:    descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{codecCast: cast},
	}
}

// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Tests in this file drive the Go code emitter end-to-end through a
// synthetic FileDescriptorProto. They exist primarily to give
// `go test` something to instrument under -coverpkg=./... so the
// generator code is reflected in the aggregate coverage report.
//
// The integration tests in lang/go/integration/ already exercise the
// generated *.codec.go output for correctness; what those tests
// cannot do is exercise the emitter itself (they consume committed
// pre-generated files). Adding a direct emission test here closes
// that loop.
//
// For deeper mutation-testing coverage on the emitter, a follow-up
// will add golden-file diffs so that any byte-level change to emitted
// Go is detected. This first pass focuses on coverage / liveness —
// no panics, no errors, non-empty output across the major branches.

package golang_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/pluginpb"

	"go.thesmos.sh/protoc-gen-codec/internal/lang/golang"
)

// codec.* extension field numbers (mirrors internal/core/options.go).
// Hand-encoded into UninterpretedOption-equivalent unknown-field bytes
// so we avoid a circular dep on the codec/options.proto package and
// don't have to regenerate it for tests.
const (
	optGoType     = 50001
	optGoField    = 50002
	optGoCast     = 50003
	optFixedLen   = 50004
	optKeepCap    = 50005
	optUsePointer = 50006
	optOneof      = 50007
)

// runGenerator builds a *protogen.Plugin from fd, drives the
// generator's exported GenerateFile entry, and returns the bytes of
// the emitted .codec.go file. Empty bytes signal nothing was
// generated (e.g. no annotated messages); the test asserts on this
// per-case.
//
// Well-known type descriptors (Timestamp / Duration) are appended to
// every request so fixtures using them resolve without additional
// per-test plumbing.
func runGenerator(t *testing.T, fd *descriptorpb.FileDescriptorProto) []byte {
	t.Helper()
	tsFD := protodesc.ToFileDescriptorProto(timestamppb.File_google_protobuf_timestamp_proto)
	durFD := protodesc.ToFileDescriptorProto(durationpb.File_google_protobuf_duration_proto)
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{fd.GetName()},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{tsFD, durFD, fd},
		CompilerVersion: &pluginpb.Version{
			Major: proto.Int32(3),
			Minor: proto.Int32(12),
		},
	}
	plugin, err := protogen.Options{}.New(req)
	if err != nil {
		t.Fatalf("protogen.New: %v", err)
	}
	if err := golang.GenerateAll(plugin); err != nil {
		t.Fatalf("GenerateAll: %v", err)
	}
	resp := plugin.Response()
	if resp.GetError() != "" {
		t.Fatalf("plugin reported error: %s", resp.GetError())
	}
	for _, f := range resp.GetFile() {
		if strings.HasSuffix(f.GetName(), ".codec.go") {
			return []byte(f.GetContent())
		}
	}
	return nil
}

// TestGenerateFile_ScalarMessage drives the simplest emit path: a
// message with a single string field. Exercises the slabNaive branch
// in unmarshal, the bool-skipping size emit, and basic nil-receiver
// guards.
func TestGenerateFile_ScalarMessage(t *testing.T) {
	t.Parallel()
	fd := buildScalarFD()
	got := runGenerator(t, fd)
	if len(got) == 0 {
		t.Fatal("expected non-empty .codec.go output for an annotated message")
	}
	for _, want := range []string{
		"func (m *M) MarshalCodec()",
		"func (m *M) MarshalToCodec(buf []byte) (int, error)",
		"func (m *M) MarshalCodecInternal(buf []byte) int",
		"func (m *M) UnmarshalCodec(data []byte) error",
		"func (m *M) UnmarshalCodecInternal(data []byte, slab string, slabOff int) error",
		"func (m *M) SizeCodec() int",
		"func (m *M) ResetCodec()",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("emitted .codec.go must contain %q (every annotated message gets the seven-method codec surface)", want)
		}
	}
}

// TestGenerateFile_NumericOnly drives the no-slab path in
// messageNeedsSlab — a message with no string/bytes/message fields
// generates UnmarshalCodec that passes "" rather than string(data) as
// the slab argument. Distinct emit branch from the slab-bearing case.
func TestGenerateFile_NumericOnly(t *testing.T) {
	t.Parallel()
	fd := &descriptorpb.FileDescriptorProto{
		Name:    new("numeric.proto"),
		Syntax:  new("proto3"),
		Package: new("t"),
		Options: &descriptorpb.FileOptions{GoPackage: new("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    new("Numbers"),
				Options: messageOptions("Numbers"),
				Field: []*descriptorpb.FieldDescriptorProto{
					int64Field("a", 1),
					basicField("b", 2, descriptorpb.FieldDescriptorProto_TYPE_UINT64),
					basicField("c", 3, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
				},
			},
		},
	}
	got := runGenerator(t, fd)
	if len(got) == 0 {
		t.Fatal("expected non-empty .codec.go output")
	}
	// No string/bytes fields → the public UnmarshalCodec wrapper
	// passes "" (not string(data)) as the slab.
	if !strings.Contains(string(got), `m.UnmarshalCodecInternal(data, "", 0)`) {
		t.Errorf(`numeric-only message must skip the slab — UnmarshalCodec should call UnmarshalCodecInternal(data, "", 0)`)
	}
}

// TestGenerateFile_NoAnnotatedMessages confirms the early-out path
// where a file's messages all lack (codec.type) — the generator
// returns nil without writing any output.
func TestGenerateFile_NoAnnotatedMessages(t *testing.T) {
	t.Parallel()
	fd := &descriptorpb.FileDescriptorProto{
		Name:    new("empty.proto"),
		Syntax:  new("proto3"),
		Package: new("t"),
		Options: &descriptorpb.FileOptions{GoPackage: new("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				// No Options → no codec.type → analyzer skips silently.
				Name: new("Untouched"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("id", 1),
				},
			},
		},
	}
	got := runGenerator(t, fd)
	if len(got) != 0 {
		t.Errorf("file with zero annotated messages must produce no output, got %d bytes", len(got))
	}
}

// TestGenerateFile_AllPaths drives a single rich FD that covers the
// major emit branches: varint scalars, repeated scalars (packed),
// repeated messages (pointer + value semantics), maps (string key,
// bool key), singular nested message, fixed-len bytes, and a
// non-synthetic oneof. The body of each case is checked for marker
// substrings; full output stability is left to the planned golden-
// file follow-up.
func TestGenerateFile_AllPaths(t *testing.T) {
	t.Parallel()
	fd := buildAllPathsFD()
	got := runGenerator(t, fd)
	src := string(got)
	if len(got) == 0 {
		t.Fatal("expected non-empty .codec.go output")
	}
	for _, marker := range []string{
		// Generated header
		"// Code generated by protoc-gen-codec-go. DO NOT EDIT.",

		// One method set per annotated message
		"func (m *Inner) MarshalCodec()",
		"func (m *AllPaths) MarshalCodec()",

		// Codec import (the generator emits this for every annotated file)
		`"go.thesmos.sh/protoc-gen-codec/lang/go/codec"`,

		// Some shape-specific emit fragments — picking lines that only
		// appear when the corresponding branch fires.
		"slices.Sort(keys)", // string-keyed map sort path
		"switch m.Kind {",   // oneof discriminator dispatch
	} {
		if !strings.Contains(src, marker) {
			t.Errorf("emitted source missing expected marker: %q", marker)
		}
	}

	// Debug aid: when the test fails, dump the generated source to
	// stderr so the failure mode is diagnosable without re-running
	// with extra flags.
	if t.Failed() {
		t.Logf("emitted source:\n%s", src)
	}
}

// ---------------------------------------------------------------------------
// FileDescriptorProto builders
// ---------------------------------------------------------------------------

// buildScalarFD returns an FD with a single message M containing one
// string field — the minimum that exercises an annotated codec emit.
func buildScalarFD() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    new("scalar.proto"),
		Syntax:  new("proto3"),
		Package: new("t"),
		Options: &descriptorpb.FileOptions{GoPackage: new("example.com/t")},
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:    new("M"),
				Options: messageOptions("M"),
				Field:   []*descriptorpb.FieldDescriptorProto{stringField("id", 1)},
			},
		},
	}
}

// buildAllPathsFD returns a comprehensive FD exercising every major
// emit branch in one file. Two annotated messages: `Inner` (the
// nested target referenced by AllPaths) and `AllPaths` itself.
//
// Map fields are sugar for `repeated <Field>Entry`. proto requires
// the synthetic entry types to be declared as nested messages on the
// parent with options.map_entry=true. We build them explicitly here
// so the descriptor resolves without protoc.
func buildAllPathsFD() *descriptorpb.FileDescriptorProto {
	oneofIdx := int32(0)
	syntheticOneofIdx := int32(1)
	proto3Optional := true
	mapEntry := true

	mapEntryDesc := func(name string, keyKind, valKind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.DescriptorProto {
		return &descriptorpb.DescriptorProto{
			Name: new(name),
			Options: &descriptorpb.MessageOptions{
				MapEntry: &mapEntry,
			},
			Field: []*descriptorpb.FieldDescriptorProto{
				{
					Name:   new("key"),
					Number: proto.Int32(1),
					Type:   keyKind.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
				{
					Name:   new("value"),
					Number: proto.Int32(2),
					Type:   valKind.Enum(),
					Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
				},
			},
		}
	}

	return &descriptorpb.FileDescriptorProto{
		Name:    new("allpaths.proto"),
		Syntax:  new("proto3"),
		Package: new("t"),
		Options: &descriptorpb.FileOptions{GoPackage: new("example.com/t")},
		Dependency: []string{
			"google/protobuf/timestamp.proto",
			"google/protobuf/duration.proto",
		},
		MessageType: []*descriptorpb.DescriptorProto{
			// Inner — referenced by AllPaths; needs its own codec.type so the
			// nested-message resolution doesn't reject it.
			{
				Name:    new("Inner"),
				Options: messageOptions("Inner"),
				Field: []*descriptorpb.FieldDescriptorProto{
					stringField("label", 1),
					int64Field("count", 2),
				},
			},

			// AllPaths — the comprehensive case.
			{
				Name: new("AllPaths"),
				Options: messageOptionsWithOneofs(
					"AllPaths",
					oneofConfig{Name: "payload", Discriminator: "Kind", Cast: "PayloadKind"},
				),
				OneofDecl: []*descriptorpb.OneofDescriptorProto{
					{Name: new("payload")},
					// proto3 optional fields each get a synthetic
					// oneof named "_<field>"; the descriptor must
					// declare them so protogen resolves correctly.
					{Name: new("_opt_int")},
				},
				NestedType: []*descriptorpb.DescriptorProto{
					mapEntryDesc("AttrsEntry",
						descriptorpb.FieldDescriptorProto_TYPE_STRING,
						descriptorpb.FieldDescriptorProto_TYPE_STRING),
					mapEntryDesc("BoolFlagsEntry",
						descriptorpb.FieldDescriptorProto_TYPE_BOOL,
						descriptorpb.FieldDescriptorProto_TYPE_STRING),
				},
				Field: []*descriptorpb.FieldDescriptorProto{
					// String + scalar varint variants.
					stringField("name", 1),
					int64Field("count", 2),
					basicField("flag", 20, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
					basicField("u32", 21, descriptorpb.FieldDescriptorProto_TYPE_UINT32),
					basicField("u64", 22, descriptorpb.FieldDescriptorProto_TYPE_UINT64),
					basicField("s32", 23, descriptorpb.FieldDescriptorProto_TYPE_SINT32),
					basicField("s64", 24, descriptorpb.FieldDescriptorProto_TYPE_SINT64),
					// Fixed-width scalar variants — exercise the
					// fixed64 / fixed32 marshal+size paths in the
					// generator.
					basicField("f64", 25, descriptorpb.FieldDescriptorProto_TYPE_FIXED64),
					basicField("sf64", 26, descriptorpb.FieldDescriptorProto_TYPE_SFIXED64),
					basicField("d", 27, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE),
					basicField("f32", 28, descriptorpb.FieldDescriptorProto_TYPE_FIXED32),
					basicField("sf32", 29, descriptorpb.FieldDescriptorProto_TYPE_SFIXED32),
					basicField("flt", 30, descriptorpb.FieldDescriptorProto_TYPE_FLOAT),
					// Variable-length bytes (separate from the
					// fixed_len bytes path tested below).
					basicField("blob", 31, descriptorpb.FieldDescriptorProto_TYPE_BYTES),
					// Repeated variants.
					packedRepeatedField("values", 3, descriptorpb.FieldDescriptorProto_TYPE_INT64),
					packedRepeatedField("fixedvals", 32, descriptorpb.FieldDescriptorProto_TYPE_FIXED64),
					packedRepeatedField("smallvals", 33, descriptorpb.FieldDescriptorProto_TYPE_FIXED32),
					repeatedScalarField("tags", 34, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					repeatedScalarField("blobs", 35, descriptorpb.FieldDescriptorProto_TYPE_BYTES),
					// Maps.
					mapField("attrs", 4, ".t.AllPaths.AttrsEntry"),
					mapField("bool_flags", 5, ".t.AllPaths.BoolFlagsEntry"),
					// Nested messages.
					singularMessageField("inner", 6, ".t.Inner"),
					repeatedMessageField("children", 7, ".t.Inner", true), // []*Inner
					repeatedMessageField("items", 8, ".t.Inner", false),   // []Inner
					// fixed_len bytes (codec.cast=Digest, fixed_len=32).
					fixedLenBytesField("digest", 9, 32),
					// proto3 optional — drives the presence-bitmap
					// path (generatePresenceFieldUnmarshal).
					{
						Name:           new("opt_int"),
						Number:         proto.Int32(40),
						Type:           descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum(),
						Label:          descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						OneofIndex:     &syntheticOneofIdx,
						Proto3Optional: &proto3Optional,
						Options:        fieldOptions("OptInt", "", 0, false, nil),
					},
					// Non-synthetic oneof branches — covering string,
					// scalar varint, bytes, fixed-width, AND nested
					// message variants of emitBranchMarshal/Size.
					oneofBranchString("text", 10, &oneofIdx),
					oneofBranchInt64("number", 11, &oneofIdx),
					oneofBranchBytes("blob_branch", 12, &oneofIdx),
					oneofBranchFixed64("amount", 13, &oneofIdx),
					oneofBranchMessage("nested", 14, ".t.Inner", &oneofIdx),
					// Well-known types — Timestamp + Duration drive
					// the dedicated WKT emit branches in
					// generateFieldMarshal / Size / Unmarshal / Reset.
					{
						Name:     new("created_at"),
						Number:   proto.Int32(50),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						TypeName: new(".google.protobuf.Timestamp"),
						Options:  fieldOptions("CreatedAt", "", 0, false, nil),
					},
					{
						Name:     new("timeout"),
						Number:   proto.Int32(51),
						Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
						Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
						TypeName: new(".google.protobuf.Duration"),
						Options:  fieldOptions("Timeout", "", 0, false, nil),
					},
				},
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Field + options helpers (hand-built to avoid pulling in the
// generated codec/options.proto package and risking a circular dep
// when the generator is being developed)
// ---------------------------------------------------------------------------

func stringField(name string, num int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func int64Field(name string, num int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func packedRepeatedField(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    kind.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

// repeatedScalarField is for non-packed-eligible repeated kinds
// (string, bytes) — proto3 marks them as repeated but they don't
// use the packed wire format; the generator emits a different path.
func repeatedScalarField(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    kind.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

// basicField is a generic singular-scalar helper used to add the many
// type variants (bool, uint*, sint*, fixed*, sfixed*, float, double,
// bytes) to the comprehensive FD without repeating the boilerplate.
func basicField(name string, num int32, kind descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    kind.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func singularMessageField(name string, num int32, typeName string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     new(name),
		Number:   new(num),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		TypeName: new(typeName),
		Options:  fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func repeatedMessageField(name string, num int32, typeName string, usePointer bool) *descriptorpb.FieldDescriptorProto {
	usePtr := usePointer
	return &descriptorpb.FieldDescriptorProto{
		Name:     new(name),
		Number:   new(num),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		TypeName: new(typeName),
		Options:  fieldOptions(uppercaseFirst(name), "", 0, false, &usePtr),
	}
}

func fixedLenBytesField(name string, num int32, length uint32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:    new(name),
		Number:  new(num),
		Type:    descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
		Label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Options: fieldOptions(uppercaseFirst(name), "Digest", length, false, nil),
	}
}

func oneofBranchString(name string, num int32, oneofIdx *int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:       new(name),
		Number:     new(num),
		Type:       descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(),
		Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		OneofIndex: oneofIdx,
		Options:    fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func oneofBranchInt64(name string, num int32, oneofIdx *int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:       new(name),
		Number:     new(num),
		Type:       descriptorpb.FieldDescriptorProto_TYPE_INT64.Enum(),
		Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		OneofIndex: oneofIdx,
		Options:    fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func oneofBranchBytes(name string, num int32, oneofIdx *int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:       new(name),
		Number:     new(num),
		Type:       descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum(),
		Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		OneofIndex: oneofIdx,
		Options:    fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func oneofBranchFixed64(name string, num int32, oneofIdx *int32) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:       new(name),
		Number:     new(num),
		Type:       descriptorpb.FieldDescriptorProto_TYPE_SFIXED64.Enum(),
		Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		OneofIndex: oneofIdx,
		Options:    fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

// oneofBranchMessage covers the nested-message branch of
// emitBranchMarshal/Size — distinct emit path from scalar branches
// because it goes through MarshalCodecInternal/SizeCodec on the
// referenced type.
func oneofBranchMessage(name string, num int32, typeName string, oneofIdx *int32) *descriptorpb.FieldDescriptorProto {
	usePtr := false
	return &descriptorpb.FieldDescriptorProto{
		Name:       new(name),
		Number:     new(num),
		Type:       descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		Label:      descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		TypeName:   new(typeName),
		OneofIndex: oneofIdx,
		Options:    fieldOptions(uppercaseFirst(name), "", 0, false, &usePtr),
	}
}

// mapField references the synthetic nested entry type by full
// proto path — the caller must declare the matching nested message
// with options.map_entry=true on the parent.
func mapField(name string, num int32, entryTypeName string) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:     new(name),
		Number:   new(num),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		TypeName: new(entryTypeName),
		Options:  fieldOptions(uppercaseFirst(name), "", 0, false, nil),
	}
}

func uppercaseFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// messageOptions encodes (codec.type) only.
func messageOptions(goType string) *descriptorpb.MessageOptions {
	return messageOptionsWithOneofs(goType)
}

// messageOptionsWithOneofs encodes (codec.type) plus zero-or-more
// (codec.oneof) entries on the unknown-fields portion of MessageOptions.
func messageOptionsWithOneofs(goType string, configs ...oneofConfig) *descriptorpb.MessageOptions {
	opts := &descriptorpb.MessageOptions{}
	raw := appendStringField(nil, optGoType, goType)
	for _, c := range configs {
		body := encodeOneofConfigBody(c)
		raw = appendBytesField(raw, optOneof, body)
	}
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

type oneofConfig struct {
	Name          string
	Discriminator string
	Cast          string
}

func encodeOneofConfigBody(c oneofConfig) []byte {
	var body []byte
	if c.Name != "" {
		body = appendStringField(body, 1, c.Name)
	}
	if c.Discriminator != "" {
		body = appendStringField(body, 2, c.Discriminator)
	}
	if c.Cast != "" {
		body = appendStringField(body, 3, c.Cast)
	}
	return body
}

// fieldOptions encodes the codec.* field annotations as raw
// unknown-field bytes on FieldOptions. Same trick as testutil_test.go
// in internal/core: we hand-encode rather than depend on the generated
// codec.options package (which would create a circular import for
// internal-package tests of the generator that emits its own consumers).
func fieldOptions(
	codecField, codecCast string,
	codecFixedLen uint32,
	codecKeepCap bool,
	codecUsePointer *bool,
) *descriptorpb.FieldOptions {
	opts := &descriptorpb.FieldOptions{}
	var raw []byte
	if codecField != "" {
		raw = appendStringField(raw, optGoField, codecField)
	}
	if codecCast != "" {
		raw = appendStringField(raw, optGoCast, codecCast)
	}
	if codecFixedLen > 0 {
		raw = appendVarintField(raw, optFixedLen, uint64(codecFixedLen))
	}
	if codecKeepCap {
		raw = appendVarintField(raw, optKeepCap, 1)
	}
	if codecUsePointer != nil {
		v := uint64(0)
		if *codecUsePointer {
			v = 1
		}
		raw = appendVarintField(raw, optUsePointer, v)
	}
	if len(raw) == 0 {
		return nil
	}
	opts.ProtoReflect().SetUnknown(raw)
	return opts
}

// ---------------------------------------------------------------------------
// proto wire-format helpers (string + varint + length-delimited bytes)
// ---------------------------------------------------------------------------

func appendStringField(raw []byte, num int32, val string) []byte {
	tag := uint64(num)<<3 | 2
	raw = appendUvarint(raw, tag)
	raw = appendUvarint(raw, uint64(len(val)))
	return append(raw, val...)
}

func appendVarintField(raw []byte, num int32, val uint64) []byte {
	raw = appendUvarint(raw, uint64(num)<<3)
	return appendUvarint(raw, val)
}

func appendBytesField(raw []byte, num int32, body []byte) []byte {
	tag := uint64(num)<<3 | 2
	raw = appendUvarint(raw, tag)
	raw = appendUvarint(raw, uint64(len(body)))
	return append(raw, body...)
}

func appendUvarint(raw []byte, v uint64) []byte {
	for v >= 0x80 {
		raw = append(raw, byte(v)|0x80)
		v >>= 7
	}
	return append(raw, byte(v))
}

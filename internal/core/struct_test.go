// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Tests in this file directly assert on the FieldInfo / MessageInfo
// values that AnalyzeMessage produces. They exist to give mutation
// testing something to detect on the analyzer's struct-population
// branches (IsBytes / IsString flags, MessageRef.ProtoFile, self-ref
// detection, codec.field name override, codec.oneof decoding).

package core

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

// IsBytes / IsString are populated from field.Desc.Kind(). The two
// equality assignments live next to each other; testing both per-kind
// pins the wiring so a swapped or negated comparison surfaces.
func TestAnalyzeField_BytesKind_SetsIsBytes(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "b", num: 1,
		kind:  descriptorpb.FieldDescriptorProto_TYPE_BYTES,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if !f.IsBytes {
		t.Errorf("FieldInfo.IsBytes must be true for a bytes-kind field")
	}
	if f.IsString {
		t.Errorf("FieldInfo.IsString must be false for a bytes-kind field")
	}
}

func TestAnalyzeField_StringKind_SetsIsString(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "s", num: 1,
		kind:  descriptorpb.FieldDescriptorProto_TYPE_STRING,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if !f.IsString {
		t.Errorf("FieldInfo.IsString must be true for a string-kind field")
	}
	if f.IsBytes {
		t.Errorf("FieldInfo.IsBytes must be false for a string-kind field")
	}
}

func TestAnalyzeField_OtherKind_SetsNeither(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "u", num: 1,
		kind:  descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if f.IsBytes {
		t.Errorf("FieldInfo.IsBytes must be false for a uint32 field")
	}
	if f.IsString {
		t.Errorf("FieldInfo.IsString must be false for a uint32 field")
	}
}

// The same-file message reference branch: declFile == file.Desc.Path()
// means the referenced message lives in the current file, so MessageRef
// .ProtoFile is left empty (no cross-package import needed). The cross-
// file branch sets it. Swapping `!=` to `==` would set ProtoFile for
// every same-file ref, which this test pins.
func TestAnalyzeField_SameFileMessageRef_LeavesProtoFileEmpty(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "m", num: 1,
		kind:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		typeName: ".t.Inner",
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if f.MessageRef == nil {
		t.Fatal("MessageRef must be populated for a message-kind field")
	}
	if f.MessageRef.ProtoFile != "" {
		t.Errorf("same-file ref: ProtoFile must be empty, got %q", f.MessageRef.ProtoFile)
	}
	if f.MessageRef.FullName != "t.Inner" {
		t.Errorf("MessageRef.FullName: got %q, want %q", f.MessageRef.FullName, "t.Inner")
	}
}

// Self-referential message field: MessageRef.FullName equals the
// containing message's FullName. The analyzer must force UsePointer=true
// regardless of cardinality, because a value-typed self-reference would
// produce an infinite-size struct. The two AND-conjuncts in
// `if fi.MessageRef != nil && msg != nil` and the equality check
// `fi.MessageRef.FullName == string(msg.Desc.FullName())` all have to
// fire for this to take effect.
func TestAnalyzeField_SelfReference_ForcesUsePointer(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeSelfRefMessage(t)
	if err != nil {
		t.Fatalf("AnalyzeMessage: %v", err)
	}
	if len(info.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(info.Fields))
	}
	f := &info.Fields[0]
	if f.MessageRef == nil || f.MessageRef.FullName != "t.M" {
		t.Fatalf("MessageRef: got %+v, want FullName=t.M", f.MessageRef)
	}
	if !f.UsePointer {
		t.Errorf("self-referential message field must have UsePointer=true (forced)")
	}
}

// Mirror test: a non-self-referential message field with explicit
// (codec.use_pointer)=false must NOT have UsePointer=true. If the
// FullName comparison were inverted (== to !=), every non-selfref would
// be detected as a selfref, forcing UsePointer=true and overriding the
// explicit false.
func TestAnalyzeField_NonSelfRef_RespectsUsePointerFalse(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "m", num: 1,
		kind:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		label:    descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		typeName: ".t.Inner",
		options:  &uninterpretedOptions{hasUsePtr: true, codecUsePtr: false},
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if f.UsePointer {
		t.Errorf("non-selfref with use_pointer=false: UsePointer must be false")
	}
}

// resolveGoName: when (codec.field) is set, TargetName must equal the
// configured name, not the auto-derived field.GoName. Mutation
// `if name != ""` → `if name == ""` would invert and use field.GoName
// even when codec.field is provided.
func TestAnalyzeField_CodecFieldOverride_TargetName(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeField(t, fieldFixture{
		name: "raw_field", num: 1,
		kind:    descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label:   descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
		options: &uninterpretedOptions{codecField: "Custom"},
	})
	if err != nil {
		t.Fatalf("AnalyzeField: %v", err)
	}
	f := &info.Fields[0]
	if f.TargetName != "Custom" {
		t.Errorf("(codec.field)=%q must override field.GoName, got TargetName=%q", "Custom", f.TargetName)
	}
}

// codec.oneof decoding: AnalyzeMessage must call messageOneofs and find
// the codec.oneof entries, building the OneofConfig map that
// analyzeField then consults. Without messageOneofs returning the
// entries, the non-synthetic oneof fields would be rejected with the
// "oneof without (codec.oneof) config" error. This test pins the
// happy path through messageOneofs and the decoded OneofInfo entry.
func TestAnalyzeMessage_WithOneofConfig_PopulatesOneofs(t *testing.T) {
	t.Parallel()
	info, err := runAnalyzeMessageWithOneofConfig(t, OneofConfig{
		Name: "value", Discriminator: "Kind", Cast: "ValueKind",
	})
	if err != nil {
		t.Fatalf("AnalyzeMessage with codec.oneof: %v", err)
	}
	if len(info.Oneofs) != 1 {
		t.Fatalf("expected 1 OneofInfo, got %d: %+v", len(info.Oneofs), info.Oneofs)
	}
	got := info.Oneofs[0]
	if got.Name != "value" || got.DiscriminatorField != "Kind" || got.DiscriminatorCast != "ValueKind" {
		t.Errorf("OneofInfo: got %+v, want {Name:value, DiscriminatorField:Kind, DiscriminatorCast:ValueKind}", got)
	}
}

// codec.oneof validation: AnalyzeMessage must reject (codec.oneof)
// entries that are missing any of the three required fields. Each
// missing field has a distinct error path so the table covers all
// three rejection branches in one shot.
func TestAnalyzeMessage_OneofConfigValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		cfg        OneofConfig
		wantSubstr string
	}{
		{
			name:       "missing name",
			cfg:        OneofConfig{Discriminator: "Kind", Cast: "ValueKind"},
			wantSubstr: "missing `name`",
		},
		{
			name:       "missing discriminator",
			cfg:        OneofConfig{Name: "value", Cast: "ValueKind"},
			wantSubstr: "requires both `discriminator` and `cast`",
		},
		{
			name:       "missing cast",
			cfg:        OneofConfig{Name: "value", Discriminator: "Kind"},
			wantSubstr: "requires both `discriminator` and `cast`",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := runAnalyzeMessageWithOneofConfig(t, c.cfg)
			if err == nil {
				t.Fatalf("AnalyzeMessage must reject (codec.oneof) with %s", c.name)
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("error %q must mention %q to identify the missing field for the user",
					err.Error(), c.wantSubstr)
			}
		})
	}
}

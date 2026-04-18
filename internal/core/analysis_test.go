// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

func TestAnalyzeMessage_MissingCodecType_SkippedSilently(t *testing.T) {
	t.Parallel()
	// A message lacking (codec.type) is not an error — it is skipped so the
	// generator can coexist with schema-only messages in a multi-purpose
	// .proto file. AnalyzeMessage returns (nil, nil).
	info, err := runAnalyzeMessage(t, false, fieldFixture{
		name: "x", num: 1,
		kind:  descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil info for unannotated message, got %+v", info)
	}
}

func TestAnalyzeField_FixedLenZero_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeField(t, fixedLenZeroFixture)
	if err == nil {
		t.Fatal("expected error for fixed_len=0")
	}
}

func TestAnalyzeField_FixedLenOnString_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeField(t, fixedLenOnStringFixture)
	if err == nil {
		t.Fatal("expected error for fixed_len on string")
	}
}

func TestResolveCast_UnresolvedAlias_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeField(t, unresolvedCastFixture)
	if err == nil {
		t.Fatal("expected error for unresolved cast alias")
	}
	if !strings.Contains(err.Error(), "unresolved cast alias") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnalyzeField_CastOnMessage_Errors(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeField(t, castOnMessageFixture)
	if err == nil {
		t.Fatal("expected error for cast on message field")
	}
}

func TestAnalyzeField_InvalidCastIdent_Errors(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"Status ", " Status", "123Status", "pkg..Type", "pkg. Type"} {
		if _, err := runAnalyzeField(t, invalidCastIdentFixture(bad)); err == nil {
			t.Fatalf("expected error for cast %q", bad)
		}
	}
}

func TestAnalyzeMessage_OneofIsRejected(t *testing.T) {
	t.Parallel()
	_, err := runAnalyzeMessageWithOneof(t)
	if err == nil {
		t.Fatal("expected error for non-synthetic oneof")
	}
	if !strings.Contains(err.Error(), "oneof") || !strings.Contains(err.Error(), "not yet supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnalyzeMessage_SyntheticOneofAllowed(t *testing.T) {
	t.Parallel()
	// This mirrors the proto3 optional path used by Task 4.1. We exercise
	// it here to confirm the oneof rejection does not over-match.
	_, err := runAnalyzeMessageWithSyntheticOneof(t)
	if err != nil {
		t.Fatalf("synthetic oneof should be allowed, got: %v", err)
	}
}

func TestAnalyzeField_ErrorMessages_NotDoublePrefixed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		fixture     fieldFixture
		contains    string
		notContains string
	}{
		{
			name:        "FixedLenZero",
			fixture:     fixedLenZeroFixture,
			contains:    "field ref: (codec.fixed_len) must be > 0",
			notContains: "field ref: field ref:",
		},
		{
			name:        "FixedLenOnString",
			fixture:     fixedLenOnStringFixture,
			contains:    "field id: (codec.fixed_len) is only valid on bytes fields",
			notContains: "field id: field id:",
		},
		{
			name:        "CastOnMessage",
			fixture:     castOnMessageFixture,
			contains:    "field m: (codec.cast) is not valid on message-type fields",
			notContains: "field m: field m:",
		},
		{
			name:        "InvalidCastIdent",
			fixture:     invalidCastIdentFixture("123bad"),
			contains:    `field x: (codec.cast) = "123bad" is not a valid identifier`,
			notContains: "field x: field x:",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := runAnalyzeField(t, c.fixture)
			if err == nil {
				t.Fatal("expected error")
			}
			msg := err.Error()
			if !strings.Contains(msg, c.contains) {
				t.Errorf("error %q must contain %q", msg, c.contains)
			}
			if strings.Contains(msg, c.notContains) {
				t.Errorf("error %q must NOT contain %q (double-prefix regression)", msg, c.notContains)
			}
		})
	}
}

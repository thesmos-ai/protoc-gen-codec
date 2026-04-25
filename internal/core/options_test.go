// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

// encodeOneofConfig hand-encodes an OneofConfig submessage body using the
// proto wire format. We can't use protobuf-go's marshaler here because that
// would require the actual generated OneofConfig type from codec/options.proto,
// which would create a circular dep. Hand-encoding mirrors what extracting
// the unknown-fields portion of an extension actually produces.
func encodeOneofConfig(name, discriminator, cast string) []byte {
	var out []byte
	emit := func(fieldNum int, val string) {
		out = protowire.AppendTag(out, protowire.Number(fieldNum), protowire.BytesType)
		out = protowire.AppendString(out, val)
	}
	if name != "" {
		emit(1, name)
	}
	if discriminator != "" {
		emit(2, discriminator)
	}
	if cast != "" {
		emit(3, cast)
	}
	return out
}

// parseOneofConfig is the inverse of the encoder above. Round-tripping every
// field through both directions pins the field-number → struct-field mapping
// AND the loop termination logic; either a swapped case-arm assignment or an
// off-by-one in the body-consumption pointer would surface as a mismatched
// OneofConfig.
func TestParseOneofConfig_AllFields_RoundtripsCleanly(t *testing.T) {
	t.Parallel()
	body := encodeOneofConfig("payload", "Kind", "PayloadKind")
	cfg, ok := parseOneofConfig(body)
	if !ok {
		t.Fatal("parseOneofConfig returned ok=false on valid input")
	}
	if cfg.Name != "payload" || cfg.Discriminator != "Kind" || cfg.Cast != "PayloadKind" {
		t.Errorf("roundtrip: got %+v, want {Name:payload, Discriminator:Kind, Cast:PayloadKind}", cfg)
	}
}

func TestParseOneofConfig_Empty_ReturnsZeroOK(t *testing.T) {
	t.Parallel()
	cfg, ok := parseOneofConfig(nil)
	if !ok {
		t.Fatal("empty body must parse as zero-cfg, ok=true (nothing malformed)")
	}
	if cfg != (OneofConfig{}) {
		t.Errorf("empty body produced non-zero cfg: %+v", cfg)
	}
}

func TestParseOneofConfig_PerField(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		body []byte
		want OneofConfig
	}{
		{"NameOnly", encodeOneofConfig("only-name", "", ""), OneofConfig{Name: "only-name"}},
		{"DiscriminatorOnly", encodeOneofConfig("", "OnlyDisc", ""), OneofConfig{Discriminator: "OnlyDisc"}},
		{"CastOnly", encodeOneofConfig("", "", "OnlyCast"), OneofConfig{Cast: "OnlyCast"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, ok := parseOneofConfig(c.body)
			if !ok {
				t.Fatal("ok=false on valid input")
			}
			if cfg != c.want {
				t.Errorf("got %+v, want %+v", cfg, c.want)
			}
		})
	}
}

// A truncated tag (high-bit set with no continuation) makes ConsumeTag return
// n < 0; parseOneofConfig must return false rather than continue or panic.
func TestParseOneofConfig_TruncatedTag_ReturnsFalse(t *testing.T) {
	t.Parallel()
	if _, ok := parseOneofConfig([]byte{0x80}); ok {
		t.Fatal("expected ok=false on truncated tag")
	}
}

// A bytes-type tag followed by a malformed length-prefixed payload triggers
// the `vn < 0` branch in the ConsumeBytes path.
func TestParseOneofConfig_TruncatedBytesPayload_ReturnsFalse(t *testing.T) {
	t.Parallel()
	// Tag for field 1, BytesType, then a varint length=5 with no follow bytes.
	body := []byte{0x0A, 0x05}
	if _, ok := parseOneofConfig(body); ok {
		t.Fatal("expected ok=false on truncated bytes payload")
	}
}

// A non-bytes wire type for an OneofConfig field is silently skipped via
// consumeFieldValue. The parser must continue to subsequent fields rather
// than return false. This exercises the `wtype != BytesType` branch.
func TestParseOneofConfig_NonBytesWireType_SkippedAndContinues(t *testing.T) {
	t.Parallel()
	// Field 99 (unknown) varint=42, then field 1 bytes="x".
	var body []byte
	body = protowire.AppendTag(body, 99, protowire.VarintType)
	body = protowire.AppendVarint(body, 42)
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendString(body, "x")
	cfg, ok := parseOneofConfig(body)
	if !ok {
		t.Fatal("non-bytes unknown field should be skipped, parse should continue")
	}
	if cfg.Name != "x" {
		t.Errorf("Name: got %q, want %q (after skipping unknown varint field)", cfg.Name, "x")
	}
}

// A non-bytes wire type with malformed payload (truncated varint) triggers
// the `consumeFieldValue returns -1` branch.
func TestParseOneofConfig_NonBytesMalformed_ReturnsFalse(t *testing.T) {
	t.Parallel()
	var body []byte
	body = protowire.AppendTag(body, 99, protowire.VarintType)
	body = append(body, 0x80) // truncated varint (high bit set, no continuation)
	if _, ok := parseOneofConfig(body); ok {
		t.Fatal("expected ok=false on truncated non-bytes payload")
	}
}

// Unknown bytes-type field numbers are intentionally ignored (forward
// compatibility), so they must NOT change cfg or return false.
func TestParseOneofConfig_UnknownBytesField_SkippedAndContinues(t *testing.T) {
	t.Parallel()
	var body []byte
	// Field 99 (not in {1,2,3}) bytes payload — must be skipped.
	body = protowire.AppendTag(body, 99, protowire.BytesType)
	body = protowire.AppendString(body, "ignored")
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendString(body, "kept")
	cfg, ok := parseOneofConfig(body)
	if !ok {
		t.Fatal("unknown bytes field should be ignored, parse should continue")
	}
	if cfg.Name != "kept" {
		t.Errorf("Name: got %q, want %q", cfg.Name, "kept")
	}
}

// oneofsFromRaw walks unknown-fields bytes scanning for codec.oneof entries.
// Each branch (no entries / single entry / multiple / interleaved with
// unknown fields) needs explicit coverage so off-by-one or branch-skip
// mutations in the wire-walking loop surface.

// makeOneofWire emits the wire-format bytes for one or more codec.oneof
// entries as they would appear in a message's unknown fields.
func makeOneofWire(cfgs ...OneofConfig) []byte {
	var out []byte
	for _, c := range cfgs {
		out = protowire.AppendTag(out, optOneof, protowire.BytesType)
		out = protowire.AppendBytes(out, encodeOneofConfig(c.Name, c.Discriminator, c.Cast))
	}
	return out
}

func TestOneofsFromRaw_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()
	if got := oneofsFromRaw(nil); got != nil {
		t.Errorf("nil raw must produce nil, got %+v", got)
	}
	if got := oneofsFromRaw([]byte{}); got != nil {
		t.Errorf("empty raw must produce nil, got %+v", got)
	}
}

func TestOneofsFromRaw_SingleEntry(t *testing.T) {
	t.Parallel()
	want := OneofConfig{Name: "p", Discriminator: "K", Cast: "T"}
	got := oneofsFromRaw(makeOneofWire(want))
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d: %+v", len(got), got)
	}
	if got[0] != want {
		t.Errorf("entry 0: got %+v, want %+v", got[0], want)
	}
}

func TestOneofsFromRaw_MultipleEntries_PreservesOrder(t *testing.T) {
	t.Parallel()
	a := OneofConfig{Name: "a", Discriminator: "AK", Cast: "AT"}
	b := OneofConfig{Name: "b", Discriminator: "BK", Cast: "BT"}
	got := oneofsFromRaw(makeOneofWire(a, b))
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0] != a || got[1] != b {
		t.Errorf("order/values: got %+v, want [%+v %+v]", got, a, b)
	}
}

// Unknown bytes-type fields (fnum != optOneof) must be skipped without
// terminating the scan.
func TestOneofsFromRaw_UnknownBytesField_SkippedAndContinues(t *testing.T) {
	t.Parallel()
	var raw []byte
	raw = protowire.AppendTag(raw, 99, protowire.BytesType)
	raw = protowire.AppendString(raw, "decoy")
	raw = append(raw, makeOneofWire(OneofConfig{Name: "real"})...)
	got := oneofsFromRaw(raw)
	if len(got) != 1 || got[0].Name != "real" {
		t.Errorf("expected single 'real' entry after decoy, got %+v", got)
	}
}

// Unknown non-bytes fields must also be skipped via consumeFieldValue.
func TestOneofsFromRaw_UnknownVarintField_SkippedAndContinues(t *testing.T) {
	t.Parallel()
	var raw []byte
	raw = protowire.AppendTag(raw, 99, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 42)
	raw = append(raw, makeOneofWire(OneofConfig{Name: "real"})...)
	got := oneofsFromRaw(raw)
	if len(got) != 1 || got[0].Name != "real" {
		t.Errorf("expected single 'real' entry after varint decoy, got %+v", got)
	}
}

// A truncated tag in the outer loop must terminate the scan and return
// whatever was collected so far. (parseOneofConfig has its own truncation
// tests; this is the messageOneofs-level equivalent.)
func TestOneofsFromRaw_TruncatedTag_ReturnsPartial(t *testing.T) {
	t.Parallel()
	raw := append(makeOneofWire(OneofConfig{Name: "first"}), 0x80) // dangling continuation byte
	got := oneofsFromRaw(raw)
	if len(got) != 1 || got[0].Name != "first" {
		t.Errorf("expected partial result with 'first', got %+v", got)
	}
}

// A codec.oneof entry with malformed body must terminate the scan.
func TestOneofsFromRaw_MalformedBody_ReturnsPartial(t *testing.T) {
	t.Parallel()
	var raw []byte
	raw = append(raw, makeOneofWire(OneofConfig{Name: "first"})...)
	raw = protowire.AppendTag(raw, optOneof, protowire.BytesType)
	raw = protowire.AppendVarint(raw, 5) // claims length 5 but no follow bytes
	got := oneofsFromRaw(raw)
	if len(got) != 1 || got[0].Name != "first" {
		t.Errorf("expected partial result with 'first', got %+v", got)
	}
}

// extractStringFromRaw / extractUint32FromRaw / extractBoolFromRaw share
// the same loop shape; tests cover the success path, the wrong-wire-type
// skip path, the absent-field path, and the malformed-payload path for
// each.

func TestExtractStringFromRaw(t *testing.T) {
	t.Parallel()
	t.Run("present returns value", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 7, protowire.BytesType)
		raw = protowire.AppendString(raw, "hello")
		if got := extractStringFromRaw(raw, 7); got != "hello" {
			t.Errorf("got %q, want %q", got, "hello")
		}
	})
	t.Run("absent returns empty", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 7, protowire.BytesType)
		raw = protowire.AppendString(raw, "hello")
		if got := extractStringFromRaw(raw, 99); got != "" {
			t.Errorf("absent: got %q, want empty", got)
		}
	})
	t.Run("non-bytes wire type for target field is skipped", func(t *testing.T) {
		// Field 7 encoded as varint, not bytes — extractString should treat
		// it as unrelated and continue scanning. Then a real bytes field 8.
		var raw []byte
		raw = protowire.AppendTag(raw, 7, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 1)
		raw = protowire.AppendTag(raw, 8, protowire.BytesType)
		raw = protowire.AppendString(raw, "real")
		if got := extractStringFromRaw(raw, 8); got != "real" {
			t.Errorf("got %q, want %q (after skipping wrong-wire-type field 7)", got, "real")
		}
	})
	t.Run("empty raw returns empty", func(t *testing.T) {
		if got := extractStringFromRaw(nil, 1); got != "" {
			t.Errorf("nil: got %q, want empty", got)
		}
	})
	t.Run("truncated tag returns empty", func(t *testing.T) {
		if got := extractStringFromRaw([]byte{0x80}, 1); got != "" {
			t.Errorf("truncated tag: got %q, want empty", got)
		}
	})
	t.Run("truncated bytes payload returns empty", func(t *testing.T) {
		if got := extractStringFromRaw([]byte{0x0A, 0x05}, 1); got != "" {
			t.Errorf("truncated payload: got %q, want empty", got)
		}
	})
	t.Run("truncated non-bytes payload returns empty", func(t *testing.T) {
		// Field 99 varint with truncated body — consumeFieldValue returns -1.
		var raw []byte
		raw = protowire.AppendTag(raw, 99, protowire.VarintType)
		raw = append(raw, 0x80) // truncated varint
		if got := extractStringFromRaw(raw, 1); got != "" {
			t.Errorf("truncated non-bytes: got %q, want empty", got)
		}
	})
}

func TestExtractUint32FromRaw(t *testing.T) {
	t.Parallel()
	t.Run("present returns value+true", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 4, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 12345)
		v, ok := extractUint32FromRaw(raw, 4)
		if !ok || v != 12345 {
			t.Errorf("got (%d, %v), want (12345, true)", v, ok)
		}
	})
	t.Run("absent returns 0+false", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 4, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 12345)
		v, ok := extractUint32FromRaw(raw, 99)
		if ok || v != 0 {
			t.Errorf("got (%d, %v), want (0, false)", v, ok)
		}
	})
	t.Run("non-varint wire type for target is skipped", func(t *testing.T) {
		// Field 4 encoded as bytes — should be skipped, then field 5 varint.
		var raw []byte
		raw = protowire.AppendTag(raw, 4, protowire.BytesType)
		raw = protowire.AppendString(raw, "decoy")
		raw = protowire.AppendTag(raw, 5, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 7)
		v, ok := extractUint32FromRaw(raw, 5)
		if !ok || v != 7 {
			t.Errorf("got (%d, %v), want (7, true)", v, ok)
		}
	})
	t.Run("empty raw returns 0+false", func(t *testing.T) {
		v, ok := extractUint32FromRaw(nil, 1)
		if ok || v != 0 {
			t.Errorf("got (%d, %v), want (0, false)", v, ok)
		}
	})
	t.Run("truncated tag returns 0+false", func(t *testing.T) {
		v, ok := extractUint32FromRaw([]byte{0x80}, 1)
		if ok || v != 0 {
			t.Errorf("truncated tag: got (%d, %v)", v, ok)
		}
	})
	t.Run("truncated varint payload returns 0+false", func(t *testing.T) {
		v, ok := extractUint32FromRaw([]byte{0x08, 0x80}, 1)
		if ok || v != 0 {
			t.Errorf("truncated varint: got (%d, %v)", v, ok)
		}
	})
}

func TestExtractBoolFromRaw(t *testing.T) {
	t.Parallel()
	t.Run("true value returns true+true", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 2, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 1)
		v, ok := extractBoolFromRaw(raw, 2)
		if !ok || !v {
			t.Errorf("got (%v, %v), want (true, true)", v, ok)
		}
	})
	t.Run("zero value returns false+true", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 2, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 0)
		v, ok := extractBoolFromRaw(raw, 2)
		if !ok || v {
			t.Errorf("got (%v, %v), want (false, true) — explicit zero is still 'present'", v, ok)
		}
	})
	t.Run("absent returns false+false", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 2, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 1)
		v, ok := extractBoolFromRaw(raw, 99)
		if ok || v {
			t.Errorf("got (%v, %v), want (false, false)", v, ok)
		}
	})
	t.Run("non-varint wire type is skipped", func(t *testing.T) {
		var raw []byte
		raw = protowire.AppendTag(raw, 2, protowire.BytesType)
		raw = protowire.AppendString(raw, "decoy")
		raw = protowire.AppendTag(raw, 3, protowire.VarintType)
		raw = protowire.AppendVarint(raw, 1)
		v, ok := extractBoolFromRaw(raw, 3)
		if !ok || !v {
			t.Errorf("got (%v, %v), want (true, true)", v, ok)
		}
	})
	t.Run("empty raw returns false+false", func(t *testing.T) {
		v, ok := extractBoolFromRaw(nil, 1)
		if ok || v {
			t.Errorf("got (%v, %v), want (false, false)", v, ok)
		}
	})
	t.Run("truncated tag returns false+false", func(t *testing.T) {
		v, ok := extractBoolFromRaw([]byte{0x80}, 1)
		if ok || v {
			t.Errorf("truncated tag: got (%v, %v)", v, ok)
		}
	})
	t.Run("truncated varint payload returns false+false", func(t *testing.T) {
		v, ok := extractBoolFromRaw([]byte{0x08, 0x80}, 1)
		if ok || v {
			t.Errorf("truncated varint: got (%v, %v)", v, ok)
		}
	})
}

// extractString / extractUint32 / extractBool wrap the *FromRaw helpers
// with a nil-message guard. These tests pin that guard so a CONDITIONALS_-
// NEGATION mutation flipping `pm == nil` to `pm != nil` (which would make
// the function return zero-value for every real input) is detected.
func TestExtract_NilProtoMessage(t *testing.T) {
	t.Parallel()
	if got := extractString(nil, 1); got != "" {
		t.Errorf("extractString(nil) = %q, want empty", got)
	}
	if v, ok := extractUint32(nil, 1); v != 0 || ok {
		t.Errorf("extractUint32(nil) = (%d, %v), want (0, false)", v, ok)
	}
	if v, ok := extractBool(nil, 1); v || ok {
		t.Errorf("extractBool(nil) = (%v, %v), want (false, false)", v, ok)
	}
}

// consumeFieldValue is the wire-type dispatcher for skipping unknown fields.
// Each branch needs explicit coverage so off-by-one mutations in the
// fixed-width length checks (`< 4`, `< 8`) surface.
func TestConsumeFieldValue(t *testing.T) {
	t.Parallel()
	t.Run("Varint returns bytes consumed", func(t *testing.T) {
		// Encode 300 as varint = 0xAC, 0x02 (2 bytes).
		buf := protowire.AppendVarint(nil, 300)
		if got := consumeFieldValue(buf, protowire.VarintType); got != 2 {
			t.Errorf("varint(300): got %d, want 2", got)
		}
	})
	t.Run("Varint truncated returns -1", func(t *testing.T) {
		if got := consumeFieldValue([]byte{0x80}, protowire.VarintType); got >= 0 {
			t.Errorf("truncated varint: got %d, want negative", got)
		}
	})
	t.Run("Fixed32 with exactly 4 bytes returns 4", func(t *testing.T) {
		if got := consumeFieldValue([]byte{1, 2, 3, 4}, protowire.Fixed32Type); got != 4 {
			t.Errorf("fixed32: got %d, want 4", got)
		}
	})
	t.Run("Fixed32 with 3 bytes returns -1", func(t *testing.T) {
		if got := consumeFieldValue([]byte{1, 2, 3}, protowire.Fixed32Type); got != -1 {
			t.Errorf("fixed32 short: got %d, want -1", got)
		}
	})
	t.Run("Fixed64 with exactly 8 bytes returns 8", func(t *testing.T) {
		if got := consumeFieldValue(make([]byte, 8), protowire.Fixed64Type); got != 8 {
			t.Errorf("fixed64: got %d, want 8", got)
		}
	})
	t.Run("Fixed64 with 7 bytes returns -1", func(t *testing.T) {
		if got := consumeFieldValue(make([]byte, 7), protowire.Fixed64Type); got != -1 {
			t.Errorf("fixed64 short: got %d, want -1", got)
		}
	})
	t.Run("BytesType valid", func(t *testing.T) {
		// Length=3 + 3 bytes = total 4 consumed.
		buf := protowire.AppendBytes(nil, []byte{1, 2, 3})
		if got := consumeFieldValue(buf, protowire.BytesType); got != 4 {
			t.Errorf("bytes: got %d, want 4", got)
		}
	})
	t.Run("BytesType truncated returns -1", func(t *testing.T) {
		// Length varint says 5, but only 1 follow-byte.
		if got := consumeFieldValue([]byte{0x05, 0xAA}, protowire.BytesType); got >= 0 {
			t.Errorf("truncated bytes: got %d, want negative", got)
		}
	})
	t.Run("StartGroupType returns -1 (unsupported)", func(t *testing.T) {
		if got := consumeFieldValue([]byte{1, 2}, protowire.StartGroupType); got != -1 {
			t.Errorf("start-group: got %d, want -1", got)
		}
	})
}

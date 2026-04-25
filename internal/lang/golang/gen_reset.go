// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.thesmos.sh/protoc-gen-codec/internal/core"
)

func generateResetCodec(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo) {
	g.P("func (m *", info.TargetType, ") ResetCodec() {")
	g.P("if m == nil {")
	g.P("return")
	g.P("}")

	for i := range info.Fields {
		f := &info.Fields[i]
		generateFieldReset(g, fileMap, f)
	}

	// Zero each oneof discriminator so a reset receiver serializes as
	// absent. Branch fields are already reset above (they live in the
	// normal field list, tagged with OneofName); the discriminator is
	// Go-only and lives outside the proto field set, so it needs its
	// own zero-write here.
	for _, oi := range info.Oneofs {
		g.P("m.", oi.DiscriminatorField, " = 0")
	}

	g.P("}")
}

func generateFieldReset(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, f *core.FieldInfo) {
	accessor := "m." + f.TargetName

	// WKT dispatch must precede IsMessage/IsMap checks; see generateFieldMarshal.
	if f.WellKnown == core.WKTTimestamp {
		g.P(accessor, " = ", identTimeTime, "{}")
		return
	}
	if f.WellKnown == core.WKTDuration {
		g.P(accessor, " = 0")
		return
	}

	// Map reset: always clear entries and preserve bucket storage. The
	// (codec.keep_capacity) annotation is a no-op on this path since
	// Phase 4.10 — retained for backward compatibility.
	if f.IsMap {
		g.P("clear(", accessor, ")")
		return
	}

	// Repeated (non-map) reset: preserve backing array capacity for reuse.
	// Recurse into element ResetCodec for nested messages so any per-element
	// pooled state is cleared in-place. The (codec.keep_capacity) annotation
	// is a no-op on this path since Phase 4.10 — retained for backward compat.
	if f.IsRepeated {
		if f.IsMessage {
			if f.UsePointer {
				g.P("for _, elem := range ", accessor, " {")
				g.P("if elem != nil { elem.ResetCodec() }")
				g.P("}")
			} else {
				g.P("for idx := range ", accessor, " {")
				g.P("(&", accessor, "[idx]).ResetCodec()")
				g.P("}")
			}
		}
		g.P(accessor, " = ", accessor, "[:0]")
		return
	}

	if f.IsMessage {
		if f.UsePointer {
			// Recurse into the nested reset so its own pooled slices/maps are
			// cleared in-place. Preserves the *T heap slot for pointer pooling
			// on the next unmarshal.
			g.P("if ", accessor, " != nil {")
			g.P(accessor, ".ResetCodec()")
			g.P("}")
		} else {
			// Value-inlined message: recurse into its ResetCodec so nested
			// slices/maps are cleared with capacity preserved.
			g.P("(&", accessor, ").ResetCodec()")
		}
		return
	}

	if f.IsProto3Optional {
		g.P(accessor, " = nil")
		return
	}

	switch {
	case f.FixedLen > 0:
		zeroType := goCastName(g, fileMap, f)
		g.P(accessor, " = ", zeroType, "{}")

	case f.IsString:
		g.P(accessor, ` = ""`)

	case f.IsBytes:
		// Variable-length bytes: preserve backing array capacity for reuse.
		// The (codec.keep_capacity) annotation is a no-op on this path since
		// Phase 4.10 — retained for backward compatibility.
		g.P(accessor, " = ", accessor, "[:0]")

	case f.ProtoKind == protoreflect.BoolKind:
		g.P(accessor, " = false")

	default:
		g.P(accessor, " = 0")
	}
}

// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"

	"go.stealthscale.io/protoc-gen-codec/internal/core"
)

// findBranchField returns the FieldInfo for the branch with the given
// proto field number, or nil if no such field exists on the message.
func findBranchField(info *core.MessageInfo, num int32) *core.FieldInfo {
	for i := range info.Fields {
		if info.Fields[i].ProtoNum == num {
			return &info.Fields[i]
		}
	}
	return nil
}

// emitOneofMarshal emits the switch-on-discriminator block that
// serializes the active branch of a non-synthetic oneof. Called at the
// end of MarshalCodecInternal after all non-branch fields are emitted.
//
// Each branch emits unconditionally (no presence guard) because the
// enclosing case IS the presence signal — if the user set the
// discriminator, the branch value is emitted even when it's the
// proto3 default (e.g. empty string), so the discriminator-intent
// round-trips.
func emitOneofMarshal(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo, oi core.OneofInfo) {
	g.P("switch m.", oi.DiscriminatorField, " {")
	for _, bn := range oi.BranchFieldNums {
		branch := findBranchField(info, bn)
		if branch == nil {
			continue
		}
		g.P("case ", oi.DiscriminatorCast, "(", bn, "):")
		emitBranchMarshal(g, fileMap, branch)
	}
	g.P("}")
}

// emitOneofSize emits the switch-on-discriminator block that computes
// the serialized-size contribution of the active branch.
func emitOneofSize(g *protogen.GeneratedFile, fileMap map[string]*protogen.File, info *core.MessageInfo, oi core.OneofInfo) {
	g.P("switch m.", oi.DiscriminatorField, " {")
	for _, bn := range oi.BranchFieldNums {
		branch := findBranchField(info, bn)
		if branch == nil {
			continue
		}
		g.P("case ", oi.DiscriminatorCast, "(", bn, "):")
		emitBranchSize(g, fileMap, branch, core.TagSize(branch.ProtoNum))
	}
	g.P("}")
}

// emitBranchMarshal emits the wire encoding for a single oneof branch
// field WITHOUT a presence guard. Called from inside a switch case arm
// of emitOneofMarshal.
//
// Branch shape restrictions (enforced by proto3):
//   - no repeated branches
//   - no map branches
//   - branches can be scalars, strings, bytes, WKTs, or singular messages
func emitBranchMarshal(g *protogen.GeneratedFile, _ map[string]*protogen.File, f *core.FieldInfo) {
	accessor := "m." + f.TargetName

	if f.WellKnown == core.WKTTimestamp {
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
		g.P("n += ", identEncodeTimestamp, "(buf[n:], ", accessor, ")")
		return
	}
	if f.WellKnown == core.WKTDuration {
		emitTag(g, f.ProtoNum, core.WireLenDel)
		g.P("sz := ", identSizeDuration, "(", accessor, ")")
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
		g.P("n += ", identEncodeDuration, "(buf[n:], ", accessor, ")")
		return
	}

	if f.IsMessage {
		emitTag(g, f.ProtoNum, core.WireLenDel)
		if f.UsePointer {
			g.P("sz := ", accessor, ".SizeCodec()")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("n += ", accessor, ".MarshalCodecInternal(buf[n:])")
		} else {
			g.P("sz := (&", accessor, ").SizeCodec()")
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(sz))")
			g.P("n += (&", accessor, ").MarshalCodecInternal(buf[n:])")
		}
		return
	}

	switch {
	case f.FixedLen > 0:
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],", f.FixedLen, ")")
		g.P("copy(buf[n:], ", accessor, "[:])")
		g.P("n += ", f.FixedLen)
	case f.Wire == core.WireVarint:
		emitTag(g, f.ProtoNum, f.Wire)
		switch f.ProtoKind {
		case protoreflect.BoolKind:
			g.P("if ", accessor, " { buf[n] = 1 } else { buf[n] = 0 }")
			g.P("n++")
		case protoreflect.Sint32Kind:
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
		case protoreflect.Sint64Kind:
			g.P("n += ", identEncodeVarint, "(buf[n:],", identZigzagEncode64, "(int64(", accessor, ")))")
		default:
			g.P("n += ", identEncodeVarint, "(buf[n:],uint64(", accessor, "))")
		}
	case f.Wire == core.WireFixed64:
		emitTag(g, f.ProtoNum, f.Wire)
		g.P(identBinaryLE, ".PutUint64(buf[n:], uint64(", accessor, "))")
		g.P("n += 8")
	case f.Wire == core.WireFixed32:
		emitTag(g, f.ProtoNum, f.Wire)
		g.P(identBinaryLE, ".PutUint32(buf[n:], uint32(", accessor, "))")
		g.P("n += 4")
	case f.IsString || f.IsBytes:
		emitTag(g, f.ProtoNum, f.Wire)
		g.P("n += ", identEncodeVarint, "(buf[n:],uint64(len(", accessor, ")))")
		g.P("n += copy(buf[n:], ", accessor, ")")
	}
}

// emitBranchSize emits the size contribution of a single oneof branch
// field without a presence guard. Called from inside a switch case arm
// of emitOneofSize.
func emitBranchSize(g *protogen.GeneratedFile, _ map[string]*protogen.File, f *core.FieldInfo, ts int) {
	accessor := "m." + f.TargetName

	if f.WellKnown == core.WKTTimestamp {
		g.P("sz := ", identSizeTimestamp, "(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
		return
	}
	if f.WellKnown == core.WKTDuration {
		g.P("sz := ", identSizeDuration, "(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
		return
	}

	if f.IsMessage {
		if f.UsePointer {
			g.P("sz := ", accessor, ".SizeCodec()")
		} else {
			g.P("sz := (&", accessor, ").SizeCodec()")
		}
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(sz)) + sz")
		return
	}

	switch {
	case f.FixedLen > 0:
		g.P("n += ", ts+core.SovLocal(uint64(f.FixedLen))+int(f.FixedLen))
	case f.Wire == core.WireVarint:
		switch f.ProtoKind {
		case protoreflect.BoolKind:
			g.P("n += ", ts+1)
		case protoreflect.Sint32Kind:
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", identZigzagEncode32, "(int32(", accessor, "))))")
		case protoreflect.Sint64Kind:
			g.P("n += ", ts, " + ", identSizeVarint, "(", identZigzagEncode64, "(int64(", accessor, ")))")
		default:
			g.P("n += ", ts, " + ", identSizeVarint, "(uint64(", accessor, "))")
		}
	case f.Wire == core.WireFixed64:
		g.P("n += ", ts+8)
	case f.Wire == core.WireFixed32:
		g.P("n += ", ts+4)
	case f.IsString || f.IsBytes:
		g.P("l := len(", accessor, ")")
		g.P("n += ", ts, " + ", identSizeVarint, "(uint64(l)) + l")
	}
}

// branchMetaFor returns the discriminator-assignment metadata for a
// branch field (its oneof's discriminator name, cast, and branch num)
// or (_, false) if the field is not a branch of a non-synthetic oneof.
func branchMetaFor(info *core.MessageInfo, f *core.FieldInfo) (oneofBranchMeta, bool) {
	if f.OneofName == "" {
		return oneofBranchMeta{}, false
	}
	for _, oi := range info.Oneofs {
		if oi.Name != f.OneofName {
			continue
		}
		return oneofBranchMeta{
			Discriminator: oi.DiscriminatorField,
			Cast:          oi.DiscriminatorCast,
			BranchNum:     f.ProtoNum,
		}, true
	}
	return oneofBranchMeta{}, false
}

type oneofBranchMeta struct {
	Discriminator string
	Cast          string
	BranchNum     int32
}

// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Package core implements language-neutral proto descriptor analysis shared
// by every protoc-gen-codec-<lang> emitter.
//
// AnalyzeMessage consumes a proto descriptor and returns a MessageInfo whose
// fields identify wire kinds, cast targets, and repeated/bytes/string
// metadata independently of any output language. Emitters convert
// MessageInfo into language-specific code.
//
// Stability: MessageInfo, FieldInfo, CastRef, WireKind, and AliasLookup are
// part of the cross-emitter contract. Adding fields is non-breaking; removing
// or repurposing them requires updating every emitter.
package core

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// validCastIdent allows letters, digits, underscore, and one dot for pkg.Type.
var validCastIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// WellKnownKind identifies a proto field whose message type is a
// well-known-type the generator has first-class support for. For such fields,
// the IsMessage/MessageRef/UsePointer flags are cleared so emitters bypass the
// generic message path and emit WKT-specific encode/decode calls instead.
type WellKnownKind int

// WellKnownKind constants enumerate the well-known types with first-class
// generator support. Extend in lockstep with analyzeField's detection switch
// and every emitter's WKT branch.
const (
	WKTNone WellKnownKind = iota
	WKTTimestamp
	WKTDuration
)

// WireKind is the proto wire format category (varint, fixed64, length-delimited, fixed32).
type WireKind int

// WireKind constants enumerate the proto wire format categories.
const (
	WireVarint  WireKind = 0
	WireFixed64 WireKind = 1
	WireLenDel  WireKind = 2
	WireFixed32 WireKind = 5
)

// AliasLookup returns the emitter-specific alias name for a dependency file.
// For the Go emitter this is string(dep.GoPackageName); other emitters
// (TypeScript module, Rust crate) provide their own mapping.
type AliasLookup func(dep *protogen.File) string

// CastRef identifies a target-language type referenced by a codec.cast
// annotation. It carries the source .proto file and the package alias the
// user wrote, so each language emitter can qualify the type in its own way.
type CastRef struct {
	// ProtoFile is the .proto file that declares the go_package / other-lang
	// package for this cast target. Empty if the cast is in the same file
	// as the referencing field (unqualified name).
	ProtoFile string
	// PackageAlias is the alias before the dot, e.g. "hash" in "hash.Digest".
	// Empty for same-file casts.
	PackageAlias string
	// Name is the unqualified type name, e.g. "Digest" or "Status".
	Name string
}

// MessageRef identifies a target-language message type referenced by a field
// of MessageKind. Each emitter resolves this to its own qualified name.
type MessageRef struct {
	// ProtoFile is the .proto file declaring the referenced message.
	// Empty if the reference is to a message in the same file.
	ProtoFile string
	// FullName is the proto full name, e.g. "t.Inner". Primarily for
	// diagnostics; emitters use TargetType for code.
	FullName string
	// TargetType is the target-language type name extracted from the
	// referenced message's (codec.type) annotation.
	TargetType string
}

// FieldInfo is the language-neutral description of one proto field after analysis.
type FieldInfo struct {
	ProtoNum     int32
	TargetName   string // emitter-specific field name on the target type
	Wire         WireKind
	ProtoKind    protoreflect.Kind
	Cast         string   // raw cast string as written in the annotation
	CastRef      *CastRef // nil if no cast; populated by resolveCast
	FixedLen     uint32
	KeepCapacity bool
	IsRepeated   bool
	IsBytes      bool
	IsString     bool
	// HasPresence is true when the field carries explicit presence information
	// (proto3 optional or message-kind).
	HasPresence bool
	// IsProto3Optional is true for proto3 optional scalars, which are
	// internally represented as a synthetic oneof. Distinguished from
	// message-kind presence.
	IsProto3Optional bool
	// IsMessage is true for nested-message fields (singular and repeated)
	// that the emitter should treat as a user-provided codec.type target.
	//
	// Contract: IsMessage and IsMap are mutually exclusive. Proto3 maps are
	// modeled as MessageKind on the wire, but AnalyzeMessage sets IsMap and
	// clears IsMessage for them so emitters can dispatch on IsMap without
	// racing against IsMessage. The invariant is asserted post-analysis.
	IsMessage bool
	// MessageRef is non-nil iff IsMessage. It identifies the referenced
	// target type, qualifying across .proto files as needed.
	MessageRef *MessageRef
	// UsePointer controls whether a message-kind field's Go target is a pointer.
	// For singular: true = *T (proto3 presence), false = T (value-inlined).
	// For repeated: true = []*T, false = []T (zero per-element alloc, default).
	// Self-referential messages are forced true. Indirect recursion
	// (A -> B -> A) is not caught; users must opt in with (codec.use_pointer).
	UsePointer bool
	// IsMap is true for proto3 map<K,V> fields. The wire format is a
	// repeated length-delimited entry message with field 1 = key and
	// field 2 = value. The Go-side target type is map[K]V.
	IsMap bool
	// MapKey describes field 1 of the synthetic entry message when IsMap.
	MapKey *FieldInfo
	// MapValue describes field 2 of the synthetic entry message when IsMap.
	MapValue *FieldInfo
	// WellKnown identifies a well-known-type field with first-class generator
	// support (e.g. google.protobuf.Timestamp -> time.Time). When non-WKTNone,
	// IsMessage/MessageRef/UsePointer are cleared so emitters bypass the
	// generic message path.
	WellKnown WellKnownKind
}

// MessageInfo describes a message's target type and field list produced by AnalyzeMessage.
type MessageInfo struct {
	TargetType string // emitter-specific type name (e.g. Go type, TS class)
	Fields     []FieldInfo
}

// AnalyzeMessage returns a MessageInfo for msg, or an error if the annotations
// are invalid. Messages lacking a (codec.type) option are skipped silently:
// AnalyzeMessage returns (nil, nil) so callers can iterate over every message
// in a file and generate only for the annotated subset. This matches the
// protoc-gen-go ergonomic where unannotated messages are a no-op.
// The aliasOf callback maps an imported proto file to the emitter's package
// alias; it is invoked while resolving codec.cast annotations that use a
// "pkg.Type" form.
func AnalyzeMessage(
	msg *protogen.Message,
	fileMap map[string]*protogen.File,
	file *protogen.File,
	aliasOf AliasLookup,
) (*MessageInfo, error) {
	targetType := messageGoType(msg)
	if targetType == "" {
		// No codec.type annotation — skip this message silently.
		// Users annotate only the messages they want codec methods on.
		return nil, nil
	}

	for _, oneof := range msg.Oneofs {
		if oneof.Desc.IsSynthetic() {
			continue // proto3 optional's synthetic oneof — allowed
		}
		return nil, fmt.Errorf(
			"message %s in %s: oneof %q is not yet supported by protoc-gen-codec; "+
				"see the plan's Phase 4.3 note for the deferred design",
			msg.Desc.Name(), file.Desc.Path(), oneof.Desc.Name(),
		)
	}

	info := &MessageInfo{TargetType: targetType}

	for _, field := range msg.Fields {
		fi, err := analyzeField(field, msg, fileMap, file, aliasOf)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", field.Desc.Name(), err)
		}
		info.Fields = append(info.Fields, fi)
	}

	return info, nil
}

func analyzeField(
	field *protogen.Field,
	msg *protogen.Message,
	fileMap map[string]*protogen.File,
	file *protogen.File,
	aliasOf AliasLookup,
) (FieldInfo, error) {
	fi := FieldInfo{
		ProtoNum:     int32(field.Desc.Number()),
		TargetName:   resolveGoName(field),
		Wire:         WireKindOf(field.Desc.Kind()),
		ProtoKind:    field.Desc.Kind(),
		KeepCapacity: fieldKeepCapacity(field),
		IsRepeated:   field.Desc.IsList(),
		IsBytes:      field.Desc.Kind() == protoreflect.BytesKind,
		IsString:     field.Desc.Kind() == protoreflect.StringKind,
		HasPresence:  field.Desc.HasPresence(),
	}
	if oneof := field.Desc.ContainingOneof(); oneof != nil && oneof.IsSynthetic() {
		fi.IsProto3Optional = true
	}

	if field.Desc.Kind() == protoreflect.MessageKind {
		fi.IsMessage = true
		msgDesc := field.Message
		if msgDesc == nil {
			return fi, errors.New("message kind with nil Message descriptor")
		}
		if !field.Desc.IsMap() {
			// Well-known types (Timestamp, Duration) lack (codec.type) but
			// have first-class generator support; detect before the missing
			// annotation is rejected.
			switch string(msgDesc.Desc.FullName()) {
			case "google.protobuf.Timestamp":
				fi.WellKnown = WKTTimestamp
				fi.IsMessage = false
				fi.UsePointer = false
			case "google.protobuf.Duration":
				fi.WellKnown = WKTDuration
				fi.IsMessage = false
				fi.UsePointer = false
			default:
				targetType := messageGoType(msgDesc)
				if targetType == "" {
					return fi, fmt.Errorf(
						"field references message %s which lacks (codec.type)",
						msgDesc.Desc.FullName(),
					)
				}
				declFile := msgDesc.Desc.ParentFile().Path()
				ref := &MessageRef{
					FullName:   string(msgDesc.Desc.FullName()),
					TargetType: targetType,
				}
				if declFile != file.Desc.Path() {
					ref.ProtoFile = declFile
				}
				fi.MessageRef = ref
			}
		}
	}

	if field.Desc.IsMap() {
		fi.IsMap = true
		// Map entries are modeled as MessageKind in proto3, but the Go-side
		// target is map[K]V, not a nested message pointer. Undo the
		// MessageKind branch so downstream emitters don't misfire.
		fi.IsMessage = false
		fi.MessageRef = nil
		// proto3 descriptors mark map fields as repeated; override for our
		// Go target so the repeated-field branch doesn't fire.
		fi.IsRepeated = false
		keyFI, err := analyzeField(field.Message.Fields[0], msg, fileMap, file, aliasOf)
		if err != nil {
			return fi, fmt.Errorf("map key: %w", err)
		}
		valFI, err := analyzeField(field.Message.Fields[1], msg, fileMap, file, aliasOf)
		if err != nil {
			return fi, fmt.Errorf("map value: %w", err)
		}
		fi.MapKey = &keyFI
		fi.MapValue = &valFI
	}

	// UsePointer resolution for message-kind fields (not maps; IsMessage is
	// unset above for maps). Cardinality-dependent default:
	//   singular -> true  (*T, proto3 presence)
	//   repeated -> false ([]T, zero per-element heap alloc)
	// Self-referential message fields are forced to true because value
	// semantics would produce an infinite-size type (direct recursion only;
	// indirect A->B->A is deferred post-v1 and users must opt in).
	if fi.IsMessage {
		if fi.IsRepeated {
			fi.UsePointer = false
		} else {
			fi.UsePointer = true
		}

		explicit, present := fieldUsePointer(field)

		isSelfRef := false
		if fi.MessageRef != nil && msg != nil {
			isSelfRef = fi.MessageRef.FullName == string(msg.Desc.FullName())
		}

		if isSelfRef {
			if present && !explicit {
				return fi, errors.New("self-referential message field requires pointer semantics; cannot set (codec.use_pointer) = false")
			}
			fi.UsePointer = true
		} else if present {
			fi.UsePointer = explicit
		}
	}

	if v, present := fieldFixedLen(field); present {
		if v == 0 {
			return fi, errors.New("(codec.fixed_len) must be > 0")
		}
		if field.Desc.Kind() != protoreflect.BytesKind {
			return fi, fmt.Errorf(
				"(codec.fixed_len) is only valid on bytes fields (got %s)",
				field.Desc.Kind(),
			)
		}
		fi.FixedLen = v
	}

	cast := fieldGoCast(field)
	fi.Cast = cast
	if cast != "" {
		if !validCastIdent.MatchString(cast) {
			return fi, fmt.Errorf(
				"(codec.cast) = %q is not a valid identifier",
				cast,
			)
		}
		if field.Desc.Kind() == protoreflect.MessageKind {
			return fi, errors.New("(codec.cast) is not valid on message-type fields")
		}
		ref, err := resolveCast(fileMap, file, cast, aliasOf)
		if err != nil {
			return fi, err
		}
		fi.CastRef = &ref
	}

	// Invariant: IsMessage and IsMap must not both be set. The map branch
	// clears IsMessage explicitly, but this guards against a future refactor
	// that sets IsMessage after the map branch.
	if fi.IsMessage && fi.IsMap {
		return fi, fmt.Errorf("internal: both IsMessage and IsMap set (field %q)", fi.TargetName)
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
	aliasOf AliasLookup,
) (CastRef, error) {
	before, after, ok := strings.Cut(cast, ".")
	if !ok {
		return CastRef{Name: cast}, nil
	}

	pkgAlias := before
	typeName := after

	for _, dep := range file.Proto.GetDependency() {
		depFile, ok := fileMap[dep]
		if !ok {
			continue
		}
		if aliasOf(depFile) == pkgAlias {
			return CastRef{
				ProtoFile:    depFile.Desc.Path(),
				PackageAlias: pkgAlias,
				Name:         typeName,
			}, nil
		}
	}

	return CastRef{}, fmt.Errorf(
		"unresolved cast alias %q in file %s: no imported proto has package alias %q",
		cast, file.Desc.Path(), pkgAlias,
	)
}

// WireKindOf returns the wire format category for a proto field kind.
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

// TagValue returns the encoded proto tag (field number + wire kind).
func TagValue(fieldNum int32, wk WireKind) uint64 {
	return uint64(fieldNum)<<3 | uint64(wk)
}

// TagSize returns the number of bytes needed to varint-encode the tag for the given field.
func TagSize(fieldNum int32) int {
	v := uint64(fieldNum) << 3
	n := 1
	for v >= 0x80 {
		n++
		v >>= 7
	}
	return n
}

// TagBytes returns the varint-encoded tag bytes for the given field and wire kind.
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

// SovLocal returns the number of bytes needed to varint-encode x.
func SovLocal(x uint64) int {
	n := 1
	for x >= 0x80 {
		n++
		x >>= 7
	}
	return n
}

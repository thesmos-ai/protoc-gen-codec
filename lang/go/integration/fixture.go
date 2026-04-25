// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Package integration contains the reference fixtures exercised by the Go
// codec test suite. The types in this file are hand-written targets for the
// generated MarshalCodec/UnmarshalCodec/SizeCodec/ResetCodec methods.
package integration

import "time"

// Status is a fixture enum exercising proto uint32-backed codec.cast.
type Status uint8

// Status values.
const (
	StatusPending   Status = 1
	StatusRunning   Status = 2
	StatusCompleted Status = 3
)

// Digest is a fixed-length byte array target for codec.fixed_len.
type Digest [32]byte

// IsZero reports whether the digest is the zero value.
func (d Digest) IsZero() bool {
	return d == Digest{}
}

// Fixed64 is a named int64 used to exercise codec.cast on sfixed64 fields.
type Fixed64 int64

// PatchKind is a fixture enum for the Patch message.
type PatchKind uint8

// PatchKind values.
const (
	PatchKindText    PatchKind = 1
	PatchKindInt     PatchKind = 2
	PatchKindFixed64 PatchKind = 3
	PatchKindBlob    PatchKind = 4
)

// Source is a fixture enum for provenance classification.
type Source uint8

// Source values.
const (
	SourceInference Source = 1
	SourceGate      Source = 2
	SourceExternal  Source = 3
)

// EvidenceKind is a fixture enum for evidence classification.
type EvidenceKind uint8

// EvidenceKind values.
const (
	EvidenceDecision       EvidenceKind = 1
	EvidenceHumanOversight EvidenceKind = 2
	EvidenceDataProvenance EvidenceKind = 3
)

// Durability is a fixture enum for persistence mode.
type Durability uint8

// Durability values.
const (
	DurabilitySoft Durability = 0
	DurabilityHard Durability = 1
)

// AccessTier is a fixture enum for access policy.
type AccessTier uint8

// AccessTier values.
const (
	AccessTierOpen      AccessTier = 1
	AccessTierTenantKey AccessTier = 2
	AccessTierDualKey   AccessTier = 3
	AccessTierSealed    AccessTier = 4
)

// Fixture exercises all scalar types, repeated strings, and bytes.
type Fixture struct {
	ID        string   `json:"id"`
	Kind      uint32   `json:"kind"`
	Status    Status   `json:"status"`
	Score     int64    `json:"score"`
	Sequence  uint64   `json:"sequence"`
	Enabled   bool     `json:"enabled"`
	Timestamp int64    `json:"timestamp"`
	Ref       Digest   `json:"ref"`
	Tags      []string `json:"tags"`
	Data      []byte   `json:"data"`
}

// Patch mirrors a sparse struct with sfixed64 and fixed-len digest.
type Patch struct {
	Kind       PatchKind `json:"kind"`
	VertexID   uint32    `json:"vertex_id"`
	Sequence   uint64    `json:"sequence"`
	Source     Source    `json:"source"`
	TextVal    string    `json:"text_val"`
	IntVal     int64     `json:"int_val"`
	Fixed64Val Fixed64   `json:"fixed64_val"`
	BlobRef    Digest    `json:"blob_ref"`
}

// Evidence is string-heavy with repeated strings and fixed-len digests.
type Evidence struct {
	Kind              EvidenceKind `json:"kind"`
	Durability        Durability   `json:"durability"`
	Access            AccessTier   `json:"access"`
	TraceID           string       `json:"trace_id"`
	FederationTraceID string       `json:"federation_trace_id"`
	JobID             string       `json:"job_id"`
	ThreadID          string       `json:"thread_id"`
	TenantID          string       `json:"tenant_id"`
	TimestampMs       int64        `json:"timestamp_ms"`
	PayloadRef        Digest       `json:"payload_ref"`
	Jurisdictions     []string     `json:"jurisdictions"`
	RetentionPolicyID string       `json:"retention_policy_id"`
}

// Minimal has a single string field (tests naive slab path).
type Minimal struct {
	ID string `json:"id"`
}

// NumericOnly has no string fields (tests no-slab path).
type NumericOnly struct {
	A uint32   `json:"a"`
	B uint64   `json:"b"`
	C int64    `json:"c"`
	D Fixed64  `json:"d"`
	E bool     `json:"e"`
	F int32    `json:"f"`
	G int64    `json:"g"`
	H *int32   `json:"h,omitempty"`
	I *bool    `json:"i,omitempty"`
	J *Fixed64 `json:"j,omitempty"`
}

// PackedZigzag exercises the packed-repeated sint path of the generator.
type PackedZigzag struct {
	Values32 []int32 `json:"values32"`
	Values64 []int64 `json:"values64"`
}

// Inner is a nested-message leaf type.
type Inner struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// Container holds a singular and a repeated nested message.
type Container struct {
	Name     string   `json:"name"`
	Inner    *Inner   `json:"inner,omitempty"`
	Children []*Inner `json:"children"`
}

// ValueContainer exercises value-semantics for both singular and repeated messages.
type ValueContainer struct {
	Name  string  `json:"name"`
	Inner Inner   `json:"inner"`
	Items []Inner `json:"items"`
}

// Tree exercises the self-reference force: a self-referential message field
// is always emitted as []*Tree because value semantics would produce an
// infinite-size type.
type Tree struct {
	Label    string  `json:"label"`
	Children []*Tree `json:"children"`
}

// MapHolder exercises proto3 map<K,V> fields.
type MapHolder struct {
	Attrs  map[string]string `json:"attrs,omitempty"`
	Counts map[string]int64  `json:"counts,omitempty"`
}

// BoolMapHolder exercises the bool-keyed map encode path. Bool keys
// have a fixed two-element domain, so the generator emits an explicit
// `false → true` sequence rather than allocating a sorted-keys slice.
type BoolMapHolder struct {
	Flags map[bool]string `json:"flags,omitempty"`
}

// TimeHolder exercises the google.protobuf.Timestamp and Duration WKT paths,
// which map to Go value types (time.Time and time.Duration) and must encode
// and decode without any heap allocations.
type TimeHolder struct {
	CreatedAt time.Time     `json:"created_at"`
	Timeout   time.Duration `json:"timeout"`
}

// BytesPool exercises (codec.keep_capacity) = true on a single []byte field.
// On warm-path unmarshal into a primed receiver, the backing array must be
// reused (decode is `append(m.Payload[:0], data...)`), leaving cap() intact
// and keeping allocs at zero.
type BytesPool struct {
	Payload []byte `json:"payload"`
}

// OneofPayloadKind is the Go-only discriminator for the OneofPayload
// oneof. Enum values equal the proto field numbers of the branches so
// the generated switch dispatches directly on Kind.
type OneofPayloadKind uint32

// OneofPayloadKind values.
const (
	OneofPayloadKindUnset  OneofPayloadKind = 0
	OneofPayloadKindText   OneofPayloadKind = 10
	OneofPayloadKindNumber OneofPayloadKind = 11
	OneofPayloadKindNested OneofPayloadKind = 12
	OneofPayloadKindBlob   OneofPayloadKind = 13
	OneofPayloadKindAmount OneofPayloadKind = 14
)

// OneofPayload exercises the non-synthetic oneof codegen path. The
// Kind field is Go-only (never serialized) and acts as the single
// source of truth for which branch is active on marshal. On decode,
// the generator sets Kind to the tag number of the branch that
// arrived on the wire.
type OneofPayload struct {
	Label string `json:"label"`

	Kind OneofPayloadKind `json:"kind"`

	// Oneof branches — only the one matching Kind is serialized.
	Text   string `json:"text,omitempty"`
	Number int64  `json:"number,omitempty"`
	Nested Inner  `json:"nested"`
	Blob   []byte `json:"blob,omitempty"`
	Amount int64  `json:"amount,omitempty"`
}

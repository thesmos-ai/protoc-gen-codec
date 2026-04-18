// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration

type Status uint8

const (
	StatusPending   Status = 1
	StatusRunning   Status = 2
	StatusCompleted Status = 3
)

type Digest [32]byte

func (d Digest) IsZero() bool {
	return d == Digest{}
}

type Fixed64 int64

type PatchKind uint8

const (
	PatchKindText    PatchKind = 1
	PatchKindInt     PatchKind = 2
	PatchKindFixed64 PatchKind = 3
	PatchKindBlob    PatchKind = 4
)

type Source uint8

const (
	SourceInference Source = 1
	SourceGate      Source = 2
	SourceExternal  Source = 3
)

type EvidenceKind uint8

const (
	EvidenceDecision       EvidenceKind = 1
	EvidenceHumanOversight EvidenceKind = 2
	EvidenceDataProvenance EvidenceKind = 3
)

type Durability uint8

const (
	DurabilitySoft Durability = 0
	DurabilityHard Durability = 1
)

type AccessTier uint8

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
	A uint32  `json:"a"`
	B uint64  `json:"b"`
	C int64   `json:"c"`
	D Fixed64 `json:"d"`
	E bool    `json:"e"`
}

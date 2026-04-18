// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Package external holds a hand-written type referenced from the
// integration package's CrossContainer fixture. Its sole purpose is to
// exercise the cross-package nested-message codegen path.
package external

// External is the cross-package nested message used by CrossContainer.
type External struct {
	Tag string `json:"tag"`
	Seq int64  `json:"seq"`
}

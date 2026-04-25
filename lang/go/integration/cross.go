// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

package integration

import "go.thesmos.sh/protoc-gen-codec/lang/go/integration/external"

// CrossContainer is the cross-package nested-message fixture.
type CrossContainer struct {
	Name     string               `json:"name"`
	Item     *external.External   `json:"item,omitempty"`
	Items    []external.External  `json:"items"`
	PtrItems []*external.External `json:"ptr_items"`
}

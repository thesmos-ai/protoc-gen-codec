// Copyright 2026 Stealth Scale B.V.
// SPDX-License-Identifier: Apache-2.0

// Direct unit tests for AssertWireSnapshot. The Assert* helpers are
// normally exercised end-to-end through RunSuite, but RunSuite's
// integration tests only hit the snapshot-matches-bytes happy path.
// The update-flag, file-missing, and bytes-mismatch branches are
// where a future refactor could silently break the dev workflow, so
// they get explicit tests here.
//
// Uses a recording stub for codectest.TB plus a tiny in-package
// fake codec.Codec implementation; runs from a per-test temp dir so
// the snapshot file IO is isolated.

package codectest

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubTB captures Helper() and Fatalf() calls so we can verify
// outcomes without driving a real *testing.T (which would terminate
// the test goroutine on Fatalf).
type stubTB struct {
	helperCalls int
	fatalfMsgs  []string
}

func (s *stubTB) Helper() { s.helperCalls++ }
func (s *stubTB) Fatalf(f string, a ...any) {
	s.fatalfMsgs = append(s.fatalfMsgs, fmt.Sprintf(f, a...))
}
func (s *stubTB) failed() bool { return len(s.fatalfMsgs) > 0 }
func (s *stubTB) firstMsg() string {
	if len(s.fatalfMsgs) == 0 {
		return ""
	}
	return s.fatalfMsgs[0]
}

// stubCodec is a minimal codec.Codec implementation backed by a
// fixed-bytes buffer. MarshalCodec returns a copy so the caller and
// the assertion share no aliased storage.
type stubCodec struct {
	wire []byte
	err  error
}

func (s *stubCodec) SizeCodec() int { return len(s.wire) }
func (s *stubCodec) MarshalCodec() ([]byte, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := make([]byte, len(s.wire))
	copy(out, s.wire)
	return out, nil
}
func (s *stubCodec) MarshalToCodec(buf []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return copy(buf, s.wire), nil
}
func (s *stubCodec) UnmarshalCodec(_ []byte) error { return nil }
func (s *stubCodec) ResetCodec()                   { s.wire = nil }

var errStubMarshalFailed = errors.New("stub marshal failed")

// chdirTo temporarily moves the test process into dir for fn.
// AssertWireSnapshot resolves snapshot paths relative to the CWD,
// so each test gets its own scratch dir.
func chdirTo(t *testing.T, dir string, fn func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	fn()
}

func TestAssertWireSnapshot_MatchingBytes_NoFatalf(t *testing.T) {
	dir := t.TempDir()
	wire := []byte{0x0a, 0x03, 'a', 'b', 'c'}

	chdirTo(t, dir, func() {
		path := filepath.Join("testdata", "wire", "stubCodec.bin")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, wire, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		tb := &stubTB{}
		AssertWireSnapshot[stubCodec, *stubCodec](tb, stubCodec{wire: wire})

		if tb.failed() {
			t.Fatalf("expected no Fatalf for matching bytes, got: %v", tb.fatalfMsgs)
		}
		if tb.helperCalls == 0 {
			t.Errorf("AssertWireSnapshot must call Helper()")
		}
	})
}

func TestAssertWireSnapshot_MissingFile_FatalfWithRefreshHint(t *testing.T) {
	dir := t.TempDir()

	chdirTo(t, dir, func() {
		tb := &stubTB{}
		AssertWireSnapshot[stubCodec, *stubCodec](tb, stubCodec{wire: []byte{0x01}})

		if !tb.failed() {
			t.Fatal("expected Fatalf when snapshot file is missing")
		}
		if !strings.Contains(tb.firstMsg(), "-update-wire-snapshots") {
			t.Errorf("missing-file fatalf must reference the -update-wire-snapshots flag; got: %s", tb.firstMsg())
		}
		if !strings.Contains(tb.firstMsg(), "testdata/wire/stubCodec.bin") {
			t.Errorf("missing-file fatalf must include the snapshot path; got: %s", tb.firstMsg())
		}
	})
}

func TestAssertWireSnapshot_BytesMismatch_FatalfWithRefreshHint(t *testing.T) {
	dir := t.TempDir()
	want := []byte{0x0a, 0x03, 'a', 'b', 'c'}
	got := []byte{0x0a, 0x03, 'x', 'y', 'z'}

	chdirTo(t, dir, func() {
		path := filepath.Join("testdata", "wire", "stubCodec.bin")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}

		tb := &stubTB{}
		AssertWireSnapshot[stubCodec, *stubCodec](tb, stubCodec{wire: got})

		if !tb.failed() {
			t.Fatal("expected Fatalf on byte mismatch")
		}
		msg := tb.firstMsg()
		if !strings.Contains(msg, "differ from") {
			t.Errorf("mismatch fatalf must say bytes differ; got: %s", msg)
		}
		if !strings.Contains(msg, "-update-wire-snapshots") {
			t.Errorf("mismatch fatalf must point at the refresh flag; got: %s", msg)
		}
	})
}

func TestAssertWireSnapshot_UpdateFlag_WritesSnapshot(t *testing.T) {
	dir := t.TempDir()
	wire := []byte{0x0a, 0x05, 'h', 'e', 'l', 'l', 'o'}

	prev := *updateWireSnapshots
	*updateWireSnapshots = true
	t.Cleanup(func() { *updateWireSnapshots = prev })

	chdirTo(t, dir, func() {
		tb := &stubTB{}
		AssertWireSnapshot[stubCodec, *stubCodec](tb, stubCodec{wire: wire})

		if tb.failed() {
			t.Fatalf("expected no Fatalf with -update flag, got: %v", tb.fatalfMsgs)
		}

		path := filepath.Join("testdata", "wire", "stubCodec.bin")
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("snapshot file not written: %v", err)
		}
		if !bytes.Equal(got, wire) {
			t.Errorf("snapshot bytes mismatch: got %x, want %x", got, wire)
		}
	})
}

func TestAssertWireSnapshot_MarshalError_Fatalf(t *testing.T) {
	dir := t.TempDir()
	chdirTo(t, dir, func() {
		tb := &stubTB{}
		AssertWireSnapshot[stubCodec, *stubCodec](tb, stubCodec{err: errStubMarshalFailed})

		if !tb.failed() {
			t.Fatal("expected Fatalf when MarshalCodec returns an error")
		}
		if !strings.Contains(tb.firstMsg(), "MarshalCodec must succeed") {
			t.Errorf("marshal-error fatalf must cite the contract; got: %s", tb.firstMsg())
		}
	})
}

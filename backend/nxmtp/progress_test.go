// SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
// Copyright (C) 2025 fratei
//
// This program is free software; you can redistribute it and/or modify it
// under the terms of the GNU General Public License version 2 as published by
// the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details.

package nxmtp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestProgressJSONContract pins the wire format of a progress payload.
//
// This exists because of a real, shipped bug: ElapsedTime is a fractional
// number of seconds, the Swift client declared it as an integer, and
// JSONDecoder rejects the *entire* payload when one field mismatches. Every
// progress update was silently discarded and the UI sat on "Preparing
// transfer…" from the first byte to the last of every transfer.
func TestProgressJSONContract(t *testing.T) {
	p := Progress{
		ElapsedTime:       0.4231234,
		Speed:             12345678.9,
		FilesSentProgress: 33.333333333333336,
		ActiveFileSize:    newFileSizeProgress(1048576, 5220000000),
		BulkFileSize:      newFileSizeProgress(1048576, 5220000000),
		Status:            StatusTransferring,
	}

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Fields that carry fractional values. A client typing any of these as an
	// integer will drop every payload.
	fractional := []string{"elapsedTime", "speed", "filesSentProgress"}
	for _, k := range fractional {
		v, ok := decoded[k]
		if !ok {
			t.Fatalf("progress payload is missing %q", k)
		}
		f, ok := v.(float64)
		if !ok {
			t.Fatalf("%q must be a JSON number, got %T", k, v)
		}
		if f == float64(int64(f)) {
			t.Fatalf("%q must be exercised with a fractional value in this test", k)
		}
	}

	for _, k := range []string{"activeFileSize", "bulkFileSize"} {
		nested, ok := decoded[k].(map[string]any)
		if !ok {
			t.Fatalf("%q must be a JSON object, got %T", k, decoded[k])
		}
		if _, ok := nested["progress"].(float64); !ok {
			t.Fatalf("%s.progress must be a JSON number", k)
		}
	}

	if _, ok := decoded["status"].(string); !ok {
		t.Fatalf("status must be a JSON string, got %T", decoded["status"])
	}
}

// TestProgressMatchesSwiftModel checks the Swift decoder declares every field
// this package emits, with a type that can actually hold it.
//
// A cross-language contract with no test is a contract that drifts. This one
// drifted once and cost a release.
func TestProgressMatchesSwiftModel(t *testing.T) {
	path := filepath.Join("..", "..", "app", "SwitchMTP", "Models", "TransferStatistics.swift")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("Swift model not present (%v)", err)
	}
	text := string(src)

	start := strings.Index(text, "struct TransferProgressData")
	if start < 0 {
		t.Fatal("TransferProgressData not found in the Swift model")
	}
	end := strings.Index(text[start:], "enum CodingKeys")
	if end < 0 {
		t.Fatal("CodingKeys not found in TransferProgressData")
	}
	block := text[start : start+end]

	decl := regexp.MustCompile(`let\s+(\w+)\s*:\s*([\w?]+)`)
	swiftTypes := map[string]string{}
	for _, m := range decl.FindAllStringSubmatch(block, -1) {
		swiftTypes[m[1]] = m[2]
	}

	// Emit a payload including every optional field so nothing is omitted.
	full := Progress{
		Note:        "note",
		Indefinite:  true,
		CurrentFile: 1,
		Status:      StatusInstalling,
	}
	raw, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Types the Swift side may use for a fractional JSON number. Int64 is the
	// trap: JSONDecoder throws rather than truncating.
	fractional := map[string]bool{"elapsedTime": true, "speed": true, "filesSentProgress": true}

	for key := range decoded {
		swiftType, ok := swiftTypes[key]
		if !ok {
			t.Errorf("Go emits %q but the Swift TransferProgressData has no matching property", key)
			continue
		}
		if fractional[key] && !strings.HasPrefix(swiftType, "Double") {
			t.Errorf("%q carries fractional values; Swift declares it %s, which drops the whole payload", key, swiftType)
		}
		if !strings.HasSuffix(swiftType, "?") {
			t.Errorf("%q is declared non-optional (%s) in Swift; an omitted field would drop the whole payload", key, swiftType)
		}
	}
}

// TestBeginFileClearsInstallingStatus guards the multi-file install case: the
// tracker must not still claim "installing" once the next file starts moving.
func TestBeginFileClearsInstallingStatus(t *testing.T) {
	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })

	tr.setStatus(StatusInstalling, "installing the first title")
	if last.Status != StatusInstalling {
		t.Fatalf("expected installing, got %q", last.Status)
	}

	tr.beginFile("second.nsp", "/second.nsp", 10)
	if last.Status != StatusTransferring {
		t.Fatalf("beginFile must return to transferring, got %q", last.Status)
	}
	if last.Note != "" {
		t.Fatalf("beginFile must clear the stale note, got %q", last.Note)
	}
}

// TestHeartbeatEmitsWhileWaiting covers the polling phases: a UI that receives
// nothing for two minutes cannot tell "waiting on the console" from "hung".
func TestHeartbeatEmitsWhileWaiting(t *testing.T) {
	var count int
	var last Progress
	tr := newProgressTracker(func(p Progress) {
		count++
		last = p
	})
	tr.setStatus(StatusInstalling, "committing")
	before := count

	tr.heartbeat()
	tr.heartbeat()

	if count != before+2 {
		t.Fatalf("expected 2 heartbeats, got %d", count-before)
	}
	if last.Status != StatusInstalling {
		t.Fatalf("heartbeat must not change the status, got %q", last.Status)
	}
	if last.Note != "committing" {
		t.Fatalf("heartbeat must not change the note, got %q", last.Note)
	}
	if last.ElapsedTime < 0 {
		t.Fatalf("elapsed time must keep advancing, got %v", time.Duration(last.ElapsedTime))
	}
}

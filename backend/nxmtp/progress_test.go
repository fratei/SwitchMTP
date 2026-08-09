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
	"sync"
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
	// Anything left at its zero value here is silently skipped by omitempty and
	// therefore never checked, so new fields must be added to this literal.
	full := Progress{
		Note:        "note",
		Indefinite:  true,
		CurrentFile: 1,
		Status:      StatusInstalling,
		Stalled:     true,
		StalledFor:  90.5,
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
	fractional := map[string]bool{"elapsedTime": true, "speed": true, "filesSentProgress": true, "stalledFor": true}

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
	defer tr.stop()

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
	defer tr.stop()
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

// withClock swaps the package clock for a controllable one so a stall can be
// tested without spending a real minute on it.
func withClock(t *testing.T, start time.Time) func(time.Duration) {
	t.Helper()
	cur := start
	real := now
	now = func() time.Time { return cur }
	t.Cleanup(func() { now = real })
	return func(d time.Duration) { cur = cur.Add(d) }
}

// TestStallReportedWhenCounterFreezes is the regression test for a 3.24 GB
// install that sat at 37% for thirteen minutes showing no error at all. The
// console had stopped draining the bulk endpoint, so the host was blocked
// inside libusb and no callback ever fired again -- from the UI's point of
// view a dead transfer was indistinguishable from a slow one.
func TestStallReportedWhenCounterFreezes(t *testing.T) {
	advanceClock := withClock(t, time.Now())

	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })
	defer tr.stop()

	tr.setTotals(1, 0, 100<<20, false)
	tr.beginFile("Cuphead.nsz", "/Cuphead.nsz", 100<<20)
	tr.advance(40 << 20)

	if last.Stalled {
		t.Fatalf("a transfer that just moved must not be stalled")
	}

	advanceClock(stallAfter + time.Second)
	tr.heartbeat()

	if !last.Stalled {
		t.Fatalf("counter still for %v must be reported as stalled", stallAfter)
	}
	if last.Note != StallNote {
		t.Fatalf("a stall must explain itself, got %q", last.Note)
	}
	if last.StalledFor < stallAfter.Seconds() {
		t.Fatalf("StalledFor = %v, want at least %v", last.StalledFor, stallAfter.Seconds())
	}

	// Recovery must clear it, otherwise the warning is a one-way door.
	advanceClock(200 * time.Millisecond)
	tr.advance(60 << 20)
	if last.Stalled {
		t.Fatalf("a recovered transfer must stop reporting a stall")
	}
}

// TestTrickleCountsAsStalled covers the failure actually observed on hardware,
// which is nastier than a clean freeze: the console kept accepting exactly one
// 16 KiB packet every 80 seconds. Any "has the byte count changed?" check sees
// movement and stays quiet, while the transfer is about 100 days from done.
func TestTrickleCountsAsStalled(t *testing.T) {
	advanceClock := withClock(t, time.Now())

	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })
	defer tr.stop()

	tr.setTotals(1, 0, 3<<30, false)
	tr.beginFile("Cuphead.nsz", "/Cuphead.nsz", 3<<30)

	sent := int64(1 << 30)
	tr.advance(sent)

	// Five rounds of the observed signature: 16 KiB, 80 seconds apart.
	for i := 0; i < 5; i++ {
		advanceClock(80 * time.Second)
		sent += 16 << 10
		tr.advance(sent)
	}

	if !last.Stalled {
		t.Fatalf("16 KiB per 80s is not progress; want stalled, got %+v", last.ActiveFileSize)
	}
}

// TestHealthyTransferNeverStalls guards the false positive. Crying stall on a
// working transfer would teach people to ignore the warning, which costs more
// than never having shown it.
func TestHealthyTransferNeverStalls(t *testing.T) {
	advanceClock := withClock(t, time.Now())

	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })
	defer tr.stop()

	tr.setTotals(1, 0, 1<<30, false)
	tr.beginFile("game.nsp", "/game.nsp", 1<<30)

	// 20 MB/s in 100ms slices, for well over the stall threshold.
	sent := int64(0)
	for i := 0; i < 10*int(stallAfter.Seconds()); i++ {
		advanceClock(100 * time.Millisecond)
		sent += 2 << 20
		tr.advance(sent)
		if last.Stalled {
			t.Fatalf("healthy transfer flagged as stalled after %d slices", i)
		}
	}
	if last.StalledFor != 0 {
		t.Fatalf("healthy transfer must not report idle time, got %v", last.StalledFor)
	}
}

// TestSlowButViableTransferNeverStalls pins the floor from the other side: a
// genuinely slow SD card is not a stalled console.
func TestSlowButViableTransferNeverStalls(t *testing.T) {
	advanceClock := withClock(t, time.Now())

	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })
	defer tr.stop()

	tr.setTotals(1, 0, 1<<30, false)
	tr.beginFile("game.nsp", "/game.nsp", 1<<30)

	// 1 MB/s -- far below healthy MTP, still comfortably above the floor.
	sent := int64(0)
	for i := 0; i < 2*int(stallAfter.Seconds()); i++ {
		advanceClock(time.Second)
		sent += 1 << 20
		tr.advance(sent)
		if last.Stalled {
			t.Fatalf("1 MB/s flagged as stalled after %ds", i)
		}
	}
}

// TestInstallingIsNotAStall matters because the install phase has no byte
// counter to move: DBI is committing and the host is right to be idle. Calling
// that a stall would fire on every single successful install.
func TestInstallingIsNotAStall(t *testing.T) {
	advanceClock := withClock(t, time.Now())

	var last Progress
	tr := newProgressTracker(func(p Progress) { last = p })
	defer tr.stop()

	tr.beginFile("game.nsp", "/game.nsp", 100)
	tr.advance(100)
	tr.setStatus(StatusInstalling, "installing")

	advanceClock(10 * stallAfter)
	tr.heartbeat()

	if last.Stalled {
		t.Fatalf("the installing phase must never be reported as stalled")
	}
	if last.Note != "installing" {
		t.Fatalf("the install note must survive, got %q", last.Note)
	}
}

// TestWatchdogEmitsDuringHardFreeze is the test that matters most, because it
// covers the case where nothing in the normal path can help: the console stops
// draining the bulk endpoint, the MTP engine blocks inside libusb, and so
// advance() is never called again. No emit is triggered by anything, and the UI
// keeps showing the last percentage it happened to see -- 37%, for thirteen
// minutes, with no error logged anywhere.
//
// This runs on the real clock with a shortened interval, so it exercises the
// watchdog goroutine and its locking for real rather than simulating it.
func TestWatchdogEmitsDuringHardFreeze(t *testing.T) {
	oldInterval, oldStall := watchInterval, stallAfter
	watchInterval, stallAfter = 10*time.Millisecond, 30*time.Millisecond
	t.Cleanup(func() { watchInterval, stallAfter = oldInterval, oldStall })

	var mu sync.Mutex
	var count int
	var stalled bool
	var note string

	tr := newProgressTracker(func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		count++
		if p.Stalled {
			stalled = true
			note = p.Note
		}
	})
	defer tr.stop()

	tr.setTotals(1, 0, 1<<30, false)
	tr.beginFile("Cuphead.nsz", "/Cuphead.nsz", 1<<30)
	tr.advance(1 << 20)

	mu.Lock()
	before := count
	mu.Unlock()

	// Now simulate the freeze by simply doing nothing at all.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	after, sawStall, sawNote := count, stalled, note
	mu.Unlock()

	if after <= before {
		t.Fatalf("watchdog produced no progress events during a freeze (%d -> %d); "+
			"the UI would sit on a stale percentage forever", before, after)
	}
	if !sawStall {
		t.Fatalf("watchdog emitted %d events but never set Stalled", after-before)
	}
	if sawNote != StallNote {
		t.Fatalf("stall must carry actionable advice, got %q", sawNote)
	}
}

// TestStopHaltsTheWatchdog guards against leaking a goroutine per transfer.
func TestStopHaltsTheWatchdog(t *testing.T) {
	oldInterval := watchInterval
	watchInterval = 5 * time.Millisecond
	t.Cleanup(func() { watchInterval = oldInterval })

	var mu sync.Mutex
	var count int
	tr := newProgressTracker(func(Progress) {
		mu.Lock()
		count++
		mu.Unlock()
	})
	tr.beginFile("game.nsp", "/game.nsp", 1<<20)

	time.Sleep(50 * time.Millisecond)
	tr.stop()
	tr.stop() // must be safe twice; callers use defer and may also stop early

	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	settled := count
	mu.Unlock()

	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	final := count
	mu.Unlock()

	if final != settled {
		t.Fatalf("watchdog kept emitting after stop (%d -> %d)", settled, final)
	}
}

// TestStopWaitsForTheWatchdog pins that no progress event can be delivered after
// stop() has returned.
//
// Closing the stop channel only asks the goroutine to leave; it may be inside
// emit() at that moment. Without a join, a heartbeat -- possibly one flagged
// Stalled -- can land after the transfer has already reported completion, so the
// UI is told a finished transfer is stuck. It also made the watchdog test race
// against its own cleanup, which is how this surfaced.
func TestStopWaitsForTheWatchdog(t *testing.T) {
	oldInterval, oldStall := watchInterval, stallAfter
	watchInterval, stallAfter = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { watchInterval, stallAfter = oldInterval, oldStall })

	var mu sync.Mutex
	var stopped bool
	var lateEmit bool

	tr := newProgressTracker(func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			lateEmit = true
		}
	})

	tr.setTotals(1, 0, 1<<30, false)
	tr.beginFile("Cuphead.nsz", "/Cuphead.nsz", 1<<30)
	tr.advance(1 << 20)

	// Let the watchdog get going so stop() has something to wait for.
	time.Sleep(20 * time.Millisecond)

	tr.stop()

	mu.Lock()
	stopped = true
	mu.Unlock()

	// If stop() returned while the goroutine was still alive, it gets a chance
	// to emit here.
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if lateEmit {
		t.Error("a progress event was delivered after stop() returned")
	}
}

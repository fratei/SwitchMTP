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
	"sync"
	"time"
)

// TransferStatus is the lifecycle state reported alongside progress.
type TransferStatus string

const (
	StatusPreprocessing TransferStatus = "preprocessing"
	StatusTransferring  TransferStatus = "transferring"
	StatusInstalling    TransferStatus = "installing"
	StatusCompleted     TransferStatus = "completed"
	StatusCancelled     TransferStatus = "cancelled"
	StatusFailed        TransferStatus = "failed"
)

// These are variables rather than constants so tests can exercise the watchdog
// without spending a real minute per case.
var (
	// stallAfter is how long the active file's byte counter may stand still
	// before a transfer is reported as stalled.
	//
	// It is deliberately generous. DBI legitimately pauses mid-transfer while
	// it commits to a slow SD card, and calling that a stall would train people
	// to ignore the warning. What it must catch is the case where the console
	// stops draining altogether -- which is indistinguishable from a healthy
	// transfer from the host's side, because the bytes simply stop being
	// accepted and libusb blocks.
	stallAfter = 60 * time.Second

	// watchInterval is how often the watchdog looks for a standing still
	// counter. It only emits when the counter has not moved, so a healthy
	// transfer never pays for it.
	watchInterval = 5 * time.Second
)

// minTransferRate is the floor, in bytes per second, below which the counter is
// treated as not really moving.
//
// A pure freeze is not the only failure mode, and it is not the worst one: a
// wedged console can dribble out one 16 KiB packet per 80 seconds, which resets
// any "did the number change?" check while being roughly 100 days away from
// finishing a 3 GB title. Bulk MTP to a Switch runs at 15-25 MB/s, so 64 KiB/s
// sits far enough below any healthy transfer to be safe and far enough above a
// trickle to catch it.
const minTransferRate = 64 * 1024

// StallNote is shown when the console stops accepting data mid-file.
//
// It says "check the console" rather than naming a cause because the host
// cannot tell the difference between DBI showing an error dialog, running out
// of space, and running out of memory in applet mode. All three look identical
// over USB: the device simply stops reading.
const StallNote = "The Switch has stopped accepting data. Check the console — DBI may be " +
	"showing an error or waiting for input. Large compressed titles can also exhaust " +
	"memory in applet mode; launching DBI by holding R while starting a game avoids that. " +
	"Cancel the transfer if the console does not recover."

// FileSizeProgress describes progress through a byte count.
type FileSizeProgress struct {
	Total    int64   `json:"total"`
	Sent     int64   `json:"sent"`
	Progress float64 `json:"progress"`
}

func newFileSizeProgress(sent, total int64) FileSizeProgress {
	p := FileSizeProgress{Total: total, Sent: sent}
	if total > 0 {
		p.Progress = float64(sent) / float64(total) * 100
		if p.Progress > 100 {
			p.Progress = 100
		}
	}
	return p
}

// Progress is the payload delivered to the progress callback.
//
// Field names are fixed by the existing Swift client.
type Progress struct {
	FullPath          string           `json:"fullPath"`
	Name              string           `json:"name"`
	ElapsedTime       float64          `json:"elapsedTime"`
	Speed             float64          `json:"speed"`
	TotalFiles        int64            `json:"totalFiles"`
	TotalDirectories  int64            `json:"totalDirectories"`
	FilesSent         int64            `json:"filesSent"`
	FilesSentProgress float64          `json:"filesSentProgress"`
	ActiveFileSize    FileSizeProgress `json:"activeFileSize"`
	BulkFileSize      FileSizeProgress `json:"bulkFileSize"`
	Status            TransferStatus   `json:"status"`

	// SwitchMTP additions.
	Note        string `json:"note,omitempty"`
	Indefinite  bool   `json:"indefinite,omitempty"`
	CurrentFile int64  `json:"currentFile,omitempty"`

	// Stalled reports that the active file's byte counter has stood still long
	// enough that the console is probably not coming back on its own.
	Stalled bool `json:"stalled,omitempty"`
	// StalledFor is how many seconds the counter has been still. It is reported
	// whenever the counter is not moving, not only once Stalled is set, so a UI
	// can show "no data for 20s" before committing to the word "stalled".
	StalledFor float64 `json:"stalledFor,omitempty"`
}

// ProgressFunc receives progress updates during a transfer.
type ProgressFunc func(Progress)

// progressTracker accumulates transfer state and rate-limits callbacks.
//
// Rate limiting matters: the MTP engine invokes its progress callback on every
// USB packet, which for a fast transfer is thousands of times per second.
// Forwarding all of those across the cgo boundary and into SwiftUI would cost
// more than the transfer itself.
type progressTracker struct {
	mu sync.Mutex

	cb       ProgressFunc
	start    time.Time
	lastEmit time.Time
	interval time.Duration

	totalFiles int64
	totalDirs  int64
	totalBytes int64

	filesSent int64
	bulkSent  int64

	activeName  string
	activePath  string
	activeTotal int64
	activeSent  int64

	status TransferStatus
	note   string
	// indefinite marks a transfer whose total size is not known in advance,
	// which happens when reading DBI's virtual storages.
	indefinite bool

	// lastAdvance is when the active file's byte counter last moved at a rate
	// that could plausibly finish, and lastAdvanceBytes is the counter's value
	// at that moment.
	lastAdvance      time.Time
	lastAdvanceBytes int64

	watchStop chan struct{}
	watchDone chan struct{}
	stopOnce  sync.Once
}

func newProgressTracker(cb ProgressFunc) *progressTracker {
	p := &progressTracker{
		cb:          cb,
		start:       now(),
		interval:    100 * time.Millisecond,
		status:      StatusTransferring,
		lastAdvance: now(),
		watchStop:   make(chan struct{}),
		watchDone:   make(chan struct{}),
	}
	if cb != nil {
		go p.watch()
	} else {
		// Nothing will ever run watch, so nothing will ever close watchDone.
		// stop() must not block waiting for a goroutine that does not exist.
		close(p.watchDone)
	}
	return p
}

// watch re-emits progress while the byte counter stands still.
//
// Without it a wedged console produces no callbacks at all: the MTP engine is
// blocked inside a libusb write, so nothing calls advance(), so nothing calls
// emit(), and the UI shows the last percentage it saw indefinitely. That is
// precisely how a dead transfer came to look like a working one sitting at 37%.
func (p *progressTracker) watch() {
	defer close(p.watchDone)
	ticker := time.NewTicker(watchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-p.watchStop:
			return
		case <-ticker.C:
			p.mu.Lock()
			idle := now().Sub(p.lastAdvance)
			moving := p.status == StatusTransferring
			p.mu.Unlock()
			// Only speak up when the counter is actually still. A healthy
			// transfer already emits every 100ms and needs no help.
			if moving && idle >= watchInterval {
				p.heartbeat()
			}
		}
	}
}

// stop halts the watchdog and waits for it to finish. Safe to call more than once.
//
// The wait matters: closing the channel only asks the goroutine to leave, and it
// may be inside emit() at that moment. Returning early lets a heartbeat -- quite
// possibly one flagged Stalled -- be delivered after the transfer has already
// reported completion, which is a confusing thing for the UI to receive. All
// callers are `defer tracker.stop()` in the transfer functions, never the
// progress callback itself, so waiting here cannot re-enter.
func (p *progressTracker) stop() {
	p.stopOnce.Do(func() { close(p.watchStop) })
	<-p.watchDone
}

func (p *progressTracker) setTotals(files, dirs, bytes int64, indefinite bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.totalFiles, p.totalDirs, p.totalBytes = files, dirs, bytes
	p.indefinite = indefinite
}

func (p *progressTracker) beginFile(name, path string, size int64) {
	p.mu.Lock()
	p.activeName, p.activePath, p.activeTotal, p.activeSent = name, path, size, 0
	// A previous file in the same batch may have left the tracker in the
	// "installing" state. Bytes are moving again, so say so.
	p.status, p.note = StatusTransferring, ""
	p.lastAdvance, p.lastAdvanceBytes = now(), 0
	p.mu.Unlock()
	p.emit(true)
}

// heartbeat re-emits the current state without changing it.
//
// It exists for the phases where the app is genuinely waiting on the console
// rather than moving bytes. Silence during those windows is indistinguishable
// from a hang, so the elapsed-time counter keeps ticking instead.
func (p *progressTracker) heartbeat() {
	p.emit(true)
}

// advance records that n more bytes of the active file have moved. The MTP
// engine reports cumulative bytes for the current object, not a delta.
func (p *progressTracker) advance(cumulative int64) {
	p.mu.Lock()
	delta := cumulative - p.activeSent
	if delta < 0 {
		delta = 0
	}
	p.activeSent = cumulative
	p.bulkSent += delta
	if delta > 0 {
		// Only reset the marker when the counter is moving fast enough to
		// finish. Treating any movement as progress is what lets a trickling
		// device masquerade as a working one.
		elapsed := now().Sub(p.lastAdvance).Seconds()
		if elapsed <= 0 || float64(cumulative-p.lastAdvanceBytes) >= elapsed*minTransferRate {
			p.lastAdvance = now()
			p.lastAdvanceBytes = cumulative
		}
	}
	p.mu.Unlock()
	p.emit(false)
}

func (p *progressTracker) endFile() {
	p.mu.Lock()
	p.filesSent++
	// If the declared size was wrong -- common on DBI's virtual storages --
	// trust the bytes we actually moved.
	if p.activeSent > p.activeTotal {
		p.activeTotal = p.activeSent
	}
	p.mu.Unlock()
	p.emit(true)
}

func (p *progressTracker) setStatus(s TransferStatus, note string) {
	p.mu.Lock()
	p.status, p.note = s, note
	p.mu.Unlock()
	p.emit(true)
}

// emit delivers a progress update, honouring the rate limit unless forced.
func (p *progressTracker) emit(force bool) {
	if p.cb == nil {
		return
	}

	p.mu.Lock()
	t := now()
	if !force && t.Sub(p.lastEmit) < p.interval {
		p.mu.Unlock()
		return
	}
	p.lastEmit = t

	elapsed := t.Sub(p.start).Seconds()
	speed := 0.0
	if elapsed > 0 {
		speed = float64(p.bulkSent) / elapsed
	}

	filesProgress := 0.0
	if p.totalFiles > 0 {
		filesProgress = float64(p.filesSent) / float64(p.totalFiles) * 100
		if filesProgress > 100 {
			filesProgress = 100
		}
	}

	bulkTotal := p.totalBytes
	if p.bulkSent > bulkTotal {
		bulkTotal = p.bulkSent
	}

	// The console is only expected to be draining the endpoint while bytes are
	// meant to be flowing. An install that has moved on to committing has no
	// counter to stand still, so it must not be called stalled.
	idle := t.Sub(p.lastAdvance)
	stalled := p.status == StatusTransferring && idle >= stallAfter
	note := p.note
	if stalled && note == "" {
		note = StallNote
	}
	// Keep the wire quiet during healthy transfers: a sub-second idle time on
	// every one of ten emits per second is noise, not information.
	stalledFor := 0.0
	if p.status == StatusTransferring && idle >= watchInterval {
		stalledFor = idle.Seconds()
	}

	prog := Progress{
		FullPath:          p.activePath,
		Name:              p.activeName,
		ElapsedTime:       elapsed,
		Speed:             speed,
		TotalFiles:        p.totalFiles,
		TotalDirectories:  p.totalDirs,
		FilesSent:         p.filesSent,
		FilesSentProgress: filesProgress,
		ActiveFileSize:    newFileSizeProgress(p.activeSent, p.activeTotal),
		BulkFileSize:      newFileSizeProgress(p.bulkSent, bulkTotal),
		Status:            p.status,
		Note:              note,
		Indefinite:        p.indefinite,
		CurrentFile:       p.filesSent + 1,
		Stalled:           stalled,
		StalledFor:        stalledFor,
	}
	cb := p.cb
	p.mu.Unlock()

	cb(prog)
}

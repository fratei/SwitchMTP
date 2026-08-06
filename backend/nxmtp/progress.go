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
}

func newProgressTracker(cb ProgressFunc) *progressTracker {
	return &progressTracker{
		cb:       cb,
		start:    now(),
		interval: 100 * time.Millisecond,
		status:   StatusTransferring,
	}
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
	p.mu.Unlock()
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
		Note:              p.note,
		Indefinite:        p.indefinite,
		CurrentFile:       p.filesSent + 1,
	}
	cb := p.cb
	p.mu.Unlock()

	cb(prog)
}

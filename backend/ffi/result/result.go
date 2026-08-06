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

// Package result implements the JSON envelope shared by every FFI callback.
//
// The shape is fixed by the existing Swift client:
//
//	{ "errorType": "<kind>", "error": "<message>", "data": <payload|null> }
//
// errorType is the empty string on success. Swift switches on it to choose a
// localised message, so the values must stay stable.
package result

import (
	"encoding/json"
	"time"
)

// Envelope is the outer structure of every callback payload.
type Envelope struct {
	ErrorType string      `json:"errorType"`
	Error     string      `json:"error"`
	Data      interface{} `json:"data"`

	// Hint carries actionable remediation text. It is additive: older clients
	// ignore it, and the Swift app renders it beneath the error message.
	Hint string `json:"hint,omitempty"`
}

// DateFormat is the timestamp layout the Swift client parses.
const DateFormat = "2006-01-02T15:04:05.000Z"

// Time wraps time.Time so it marshals in the exact format Swift expects.
// Encoding a zero time as an empty string lets the client distinguish
// "unknown" from "the epoch", which matters because MTP responders frequently
// report no timestamp at all.
type Time struct{ time.Time }

// MarshalJSON renders the timestamp in UTC using DateFormat.
func (t Time) MarshalJSON() ([]byte, error) {
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.UTC().Format(DateFormat))
}

// UnmarshalJSON accepts DateFormat, RFC 3339, or an empty string.
func (t *Time) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	for _, layout := range []string{DateFormat, time.RFC3339Nano, time.RFC3339} {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	t.Time = time.Time{}
	return nil
}

// Success builds a success envelope and returns it as JSON.
func Success(data interface{}) string {
	return marshal(Envelope{ErrorType: "", Error: "", Data: data})
}

// Failure builds an error envelope.
func Failure(errorType, message, hint string) string {
	return marshal(Envelope{ErrorType: errorType, Error: message, Hint: hint, Data: nil})
}

// marshal serialises an envelope, falling back to a hand-built error payload
// if serialisation itself fails.
//
// This must never panic and must never return an empty string: the Swift side
// treats an unparseable callback payload as a fatal protocol error, and a
// marshalling bug in a corner case would otherwise look like a crash.
func marshal(e Envelope) string {
	b, err := json.Marshal(e)
	if err != nil {
		fallback, ferr := json.Marshal(Envelope{
			ErrorType: "internal",
			Error:     "failed to encode response: " + err.Error(),
		})
		if ferr != nil {
			return `{"errorType":"internal","error":"failed to encode response","data":null}`
		}
		return string(fallback)
	}
	return string(b)
}

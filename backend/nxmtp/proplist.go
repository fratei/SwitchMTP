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
	"bytes"
	"encoding/binary"
	"io"
	"time"

	"github.com/ganeshrvel/go-mtpfs/mtp"
)

// GetObjectPropList (MTP operation 0x9805) returns the properties of every
// object in a directory in a single transaction. On USB that is worth a great
// deal: the fallback costs one round trip per entry, so a folder with 500
// files takes 500 round trips instead of one.
//
// The engine we vendor does not wrap this operation, so we drive it through
// RunTransaction and decode the ObjectPropList dataset ourselves.
//
// Support is optional and DBI's implementation is undocumented, so every
// failure path here must return an unsupported error rather than a hard one --
// the caller then permanently falls back to GetObjectHandles + GetObjectInfo.

const (
	// propAll requests every property the device knows for the object.
	propAll uint32 = 0xFFFFFFFF
	// depthImmediate asks for the direct children of the given object.
	depthImmediate uint32 = 1
)

// listViaPropList fetches a directory's entries in one transaction.
func (c *Client) listViaPropList(storageID, parent uint32) ([]rawEntry, error) {
	var buf bytes.Buffer
	req := mtp.Container{
		Code: mtp.OC_MTP_GetObjPropList,
		Param: []uint32{
			parent,         // object whose children we want
			0,              // all object formats
			propAll,        // all properties
			0,              // no group code
			depthImmediate, // immediate children only
		},
	}
	var rep mtp.Container
	if err := c.t.RunTransaction(&req, &rep, &buf, nil, 0, nil); err != nil {
		return nil, err
	}

	props, err := decodeObjectPropList(&buf)
	if err != nil {
		// A dataset we cannot parse is indistinguishable, from the caller's
		// point of view, from the operation not being supported: either way we
		// must use the fallback.
		return nil, newErr(KindUnsupported, "getObjectPropList", "unparseable property list: "+err.Error())
	}

	byHandle := make(map[uint32]*rawEntry)
	order := make([]uint32, 0, len(props))

	for _, p := range props {
		e, ok := byHandle[p.Handle]
		if !ok {
			e = &rawEntry{Handle: p.Handle, Parent: parent}
			byHandle[p.Handle] = e
			order = append(order, p.Handle)
		}
		applyProp(e, p)
	}

	entries := make([]rawEntry, 0, len(order))
	for _, h := range order {
		e := byHandle[h]
		if e.Name == "" {
			// Without a name the entry is useless; fall back wholesale rather
			// than present a half-decoded directory.
			return nil, newErr(KindUnsupported, "getObjectPropList", "device omitted object names")
		}
		if e.IsFolder {
			e.Size = 0
		}
		entries = append(entries, *e)
	}
	return entries, nil
}

// objectProp is one decoded (handle, property, value) triple.
type objectProp struct {
	Handle   uint32
	Code     uint16
	DataType uint16
	U64      uint64
	Str      string
	IsStr    bool
}

// decodeObjectPropList parses the ObjectPropList dataset:
//
//	uint32 NumberOfElements
//	repeated { uint32 ObjectHandle, uint16 PropertyCode, uint16 DataType, <value> }
func decodeObjectPropList(r io.Reader) ([]objectProp, error) {
	var count uint32
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, err
	}
	// Guard against a garbled length claiming an absurd number of elements.
	const maxElements = 4 << 20
	if count > maxElements {
		return nil, io.ErrUnexpectedEOF
	}

	out := make([]objectProp, 0, count)
	for i := uint32(0); i < count; i++ {
		var p objectProp
		if err := binary.Read(r, binary.LittleEndian, &p.Handle); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &p.Code); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &p.DataType); err != nil {
			return nil, err
		}
		if err := decodePropValue(r, &p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// decodePropValue reads a single value whose type is given by p.DataType.
func decodePropValue(r io.Reader, p *objectProp) error {
	switch p.DataType {
	case mtp.DTC_INT8, mtp.DTC_UINT8:
		var v uint8
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		p.U64 = uint64(v)
	case mtp.DTC_INT16, mtp.DTC_UINT16:
		var v uint16
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		p.U64 = uint64(v)
	case mtp.DTC_INT32, mtp.DTC_UINT32:
		var v uint32
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		p.U64 = uint64(v)
	case mtp.DTC_INT64, mtp.DTC_UINT64:
		var v uint64
		if err := binary.Read(r, binary.LittleEndian, &v); err != nil {
			return err
		}
		p.U64 = v
	case mtp.DTC_INT128, mtp.DTC_UINT128:
		var v [16]byte
		if _, err := io.ReadFull(r, v[:]); err != nil {
			return err
		}
		p.U64 = binary.LittleEndian.Uint64(v[:8])
	case mtp.DTC_STR:
		s, err := decodeMTPString(r)
		if err != nil {
			return err
		}
		p.Str, p.IsStr = s, true
	default:
		// Arrays and unknown types cannot be skipped safely, because we do not
		// know their length. Bail out and let the caller use the fallback.
		return io.ErrUnexpectedEOF
	}
	return nil
}

// decodeMTPString reads an MTP string: a uint8 character count followed by
// that many UTF-16LE code units, NUL-terminated. A count of 0 means the empty
// string and is not followed by any data.
func decodeMTPString(r io.Reader) (string, error) {
	var n uint8
	if err := binary.Read(r, binary.LittleEndian, &n); err != nil {
		return "", err
	}
	if n == 0 {
		return "", nil
	}
	units := make([]uint16, n)
	if err := binary.Read(r, binary.LittleEndian, &units); err != nil {
		return "", err
	}
	// Drop the trailing NUL if present.
	if len(units) > 0 && units[len(units)-1] == 0 {
		units = units[:len(units)-1]
	}
	return decodeUTF16(units), nil
}

// decodeUTF16 converts UTF-16LE code units to a Go string, correctly joining
// surrogate pairs. Filenames on a Switch SD card routinely contain emoji and
// CJK characters, so this has to be right.
func decodeUTF16(units []uint16) string {
	runes := make([]rune, 0, len(units))
	for i := 0; i < len(units); i++ {
		u := units[i]
		switch {
		case u >= 0xD800 && u < 0xDC00 && i+1 < len(units):
			lo := units[i+1]
			if lo >= 0xDC00 && lo < 0xE000 {
				runes = append(runes, ((rune(u)-0xD800)<<10|(rune(lo)-0xDC00))+0x10000)
				i++
				continue
			}
			runes = append(runes, '\uFFFD')
		case u >= 0xD800 && u < 0xE000:
			runes = append(runes, '\uFFFD')
		default:
			runes = append(runes, rune(u))
		}
	}
	return string(runes)
}

// applyProp folds one decoded property into the entry being built.
func applyProp(e *rawEntry, p objectProp) {
	switch p.Code {
	case mtp.OPC_ObjectFileName:
		if p.IsStr && p.Str != "" {
			e.Name = p.Str
		}
	case mtp.OPC_Name:
		if p.IsStr && e.Name == "" {
			e.Name = p.Str
		}
	case mtp.OPC_ObjectFormat:
		e.Format = uint16(p.U64)
		e.IsFolder = isAssociation(e.Format)
	case mtp.OPC_ObjectSize:
		if p.U64 >= uint64(sizeOverflow) && p.DataType != mtp.DTC_UINT64 && p.DataType != mtp.DTC_INT64 {
			// A 32-bit size field pinned at 0xFFFFFFFF is an overflow marker,
			// not a real size.
			e.SizeUnknown = true
			e.Size = 0
		} else {
			e.Size = int64(p.U64)
			e.SizeUnknown = false
		}
	case mtp.OPC_ParentObject:
		if p.U64 != 0 {
			e.Parent = uint32(p.U64)
		}
	case mtp.OPC_DateModified:
		if p.IsStr {
			if t, err := parseMTPDate(p.Str); err == nil {
				e.Modified = t
			}
		}
	}
}

// parseMTPDate parses MTP's date format, "YYYYMMDDThhmmss" with an optional
// fractional part and an optional trailing Z or +/-hhmm offset.
func parseMTPDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, io.EOF
	}
	layouts := []string{
		"20060102T150405.0Z0700",
		"20060102T150405Z0700",
		"20060102T150405.0",
		"20060102T150405",
		"20060102T1504",
	}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

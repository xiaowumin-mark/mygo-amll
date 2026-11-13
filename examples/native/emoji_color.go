// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"os"

	"github.com/go-text/typesetting/font"
)

// The color-font owner is independent of the effect renderer's 64 MiB texture
// estimate. Source bytes and table counts have hard admission limits; parsed
// retained bytes use an explicit conservative estimate, not a heap/RSS promise.
const (
	emojiFontSourceLimit   = 16 << 20
	emojiFontRetainedLimit = 64 << 20
	emojiFontTableLimit    = 128
	emojiFontBaseLimit     = 16384
	emojiFontLayerLimit    = 262144
)

var (
	errEmojiColorLimit       = errors.New("native color font: resource budget exceeded")
	errEmojiColorUnsupported = errors.New("native color font: unsupported paint or font form")
	errEmojiColorClosed      = errors.New("native color font: owner is closed")
	errEmojiColorInvalid     = errors.New("native color font: invalid font or paint data")
)

// referenceColorGlyph is created by our own public go-text shaper, never by
// reading or constructing private ui.Glyph fields. X/Y are relative to the text
// baseline, in the requested Size's units; Y increases down, including offsets.
type referenceColorGlyph struct {
	Font       *emojiColorFont
	ID         font.GID
	X, Y, Size float32
}

type emojiColorRaster struct {
	Pixels *image.RGBA
	Origin image.Point // physical pixels relative to the text baseline
}

type emojiFontStats struct {
	SourceBytes, EstimatedRetainedBytes int64
	Entries                             int
	ID                                  [32]byte
}

// emojiColorFont owns one parsed local font. Construct it explicitly before a
// window/frame; no default constructor or Draw path performs font I/O. It is
// confined to the UI/cold-preparation thread. The scene must first release all
// geometries and borrowed shaping Faces, then Close this owner.
type emojiColorFont struct {
	face       *font.Face
	source     emojiPaintSource
	glyphCount uint32
	stats      emojiFontStats
}

func openEmojiColorFont(filename string) (result *emojiColorFont, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			result, err = nil, fmt.Errorf("%w: parser rejected font: %v", errEmojiColorInvalid, failure)
		}
	}()
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("native color font: open: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("native color font: stat: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > emojiFontSourceLimit {
		return nil, errEmojiColorLimit
	}
	// This estimate includes a source-sized allocation and parsed-table/cache
	// allowance. The checked-in validation records separately report actual heap
	// observations and cold parse allocations; this is not a peak-memory cap.
	estimated := info.Size()*5 + (2 << 20)
	if estimated > emojiFontRetainedLimit {
		return nil, errEmojiColorLimit
	}
	data, err := io.ReadAll(io.LimitReader(file, emojiFontSourceLimit+1))
	if err != nil {
		return nil, fmt.Errorf("native color font: read: %w", err)
	}
	if int64(len(data)) != info.Size() || len(data) > emojiFontSourceLimit {
		return nil, errEmojiColorLimit
	}
	if err := preflightEmojiFont(data); err != nil {
		return nil, err
	}
	glyphCount, err := emojiFontGlyphCount(data)
	if err != nil {
		return nil, err
	}
	face, err := font.ParseTTF(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: parse: %v", errEmojiColorInvalid, err)
	}
	if face.Upem() == 0 || face.COLR == nil || len(face.CPAL) == 0 || len(face.CPAL[0]) == 0 {
		return nil, errEmojiColorUnsupported
	}
	result = &emojiColorFont{face: face, source: emojiFaceSource{face}, glyphCount: glyphCount, stats: emojiFontStats{SourceBytes: int64(len(data)), EstimatedRetainedBytes: estimated, Entries: 1, ID: sha256.Sum256(data)}}
	// ParseTTF's public Font owns its table buffers. We do not retain a second
	// complete copy of the input or register an unremovable process-global alias.
	return result, nil
}

func (f *emojiColorFont) newShapingFace() *font.Face {
	if f == nil || f.face == nil {
		return nil
	}
	// Font is documented immutable/concurrent-readable; each Face owns mutable
	// shaping caches. The borrower must obey the owner's lifetime and thread.
	return font.NewFace(f.face.Font)
}

func (f *emojiColorFont) Stats() emojiFontStats {
	if f == nil {
		return emojiFontStats{}
	}
	return f.stats
}

func (f *emojiColorFont) Close() {
	if f != nil {
		f.face = nil
		f.source = nil
		f.glyphCount = 0
		f.stats = emojiFontStats{}
	}
}

func emojiFontGlyphCount(data []byte) (uint32, error) {
	count := int(binary.BigEndian.Uint16(data[4:6])) // directory validated above
	for i := 0; i < count; i++ {
		record := data[12+i*16 : 28+i*16]
		if binary.BigEndian.Uint32(record[:4]) != 0x6d617870 { // maxp
			continue
		}
		offset, size := int(binary.BigEndian.Uint32(record[8:12])), int(binary.BigEndian.Uint32(record[12:16]))
		if size < 6 {
			return 0, errEmojiColorInvalid
		}
		glyphs := uint32(binary.BigEndian.Uint16(data[offset+4 : offset+6]))
		if glyphs == 0 {
			return 0, errEmojiColorInvalid
		}
		return glyphs, nil
	}
	return 0, errEmojiColorInvalid
}

func preflightEmojiFont(data []byte) error {
	if len(data) < 12 {
		return errEmojiColorInvalid
	}
	version := binary.BigEndian.Uint32(data[:4])
	if version != 0x00010000 && version != 0x4f54544f { // TrueType / OTTO
		return errEmojiColorUnsupported
	}
	count := int(binary.BigEndian.Uint16(data[4:6]))
	if count == 0 || count > emojiFontTableLimit || count > (len(data)-12)/16 {
		return errEmojiColorLimit
	}
	seen := make(map[uint32]bool, count)
	for i := 0; i < count; i++ {
		record := data[12+i*16 : 28+i*16]
		tag := binary.BigEndian.Uint32(record[:4])
		offset, size := uint64(binary.BigEndian.Uint32(record[8:12])), uint64(binary.BigEndian.Uint32(record[12:16]))
		if seen[tag] || offset > uint64(len(data)) || size > uint64(len(data))-offset {
			return errEmojiColorInvalid
		}
		seen[tag] = true
		if tag != 0x434f4c52 { // COLR
			continue
		}
		table := data[int(offset):int(offset+size)]
		if len(table) < 14 {
			return errEmojiColorInvalid
		}
		v := binary.BigEndian.Uint16(table[:2])
		if v > 1 {
			return errEmojiColorUnsupported
		}
		if v == 1 {
			if len(table) < 34 {
				return errEmojiColorInvalid
			}
			for _, field := range []struct{ offset, maximum int }{{14, emojiFontBaseLimit}, {18, emojiFontLayerLimit}} {
				at := uint64(binary.BigEndian.Uint32(table[field.offset : field.offset+4]))
				if at == 0 {
					continue
				}
				if at > uint64(len(table)) || uint64(len(table))-at < 4 {
					return errEmojiColorInvalid
				}
				entries := uint64(binary.BigEndian.Uint32(table[int(at) : int(at)+4]))
				if entries > uint64(field.maximum) {
					return errEmojiColorLimit
				}
			}
		}
	}
	if !seen[0x434f4c52] || !seen[0x4350414c] { // COLR / CPAL
		return errEmojiColorUnsupported
	}
	return nil
}

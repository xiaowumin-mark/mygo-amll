// Package ttml parses an explicitly limited, in-memory TTML lyric profile.
//
// Supported input is UTF-8 TTML (the W3C namespace, its legacy namespace, or
// unqualified elements), with tt/head/metadata/body/div/p/span/br, xml:id,
// xml:lang, xml:space, ttm:agent, and inline x-bg, x-translation and x-roman
// roles. Background spans become independent overlapping lines; nested x-bg
// roles are rejected, including nesting through ordinary spans. Agent v2 is
// the alternate duet side; other agent identifiers are retained verbatim.
// AMLL meta key/value properties and ttm:title/name are retained as properties.
//
// This profile interprets p/span begin/end as absolute song positions. Clock
// expressions hh:mm:ss.f and mm:ss.f and decimal h/m/s/ms offsets are supported
// to nanosecond precision. Timing on body/div, sequential containers, frame/tick
// clocks, DTDs, entities declared by a DTD, media and external references are
// rejected. Styles, layout, ruby and unknown extensions produce diagnostics;
// this package does not claim general TTML or complete Apple Music support.
//
// Missing line ends use the common format normalization policy. dur is relative
// to its element's begin. There is no TTML offset extension in this profile.
// Default XML whitespace is collapsed; xml:space="preserve" keeps lyric text.
// Diagnostics use one-based physical lines and UTF-8 byte columns. Parse only
// reads the supplied bytes and does not access files, URLs or a network.
package ttml

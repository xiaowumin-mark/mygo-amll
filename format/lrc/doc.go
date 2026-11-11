// Package lrc parses UTF-8 LRC and enhanced LRC from caller-provided bytes.
// It performs no I/O. A leading UTF-8 BOM and LF, CRLF, and CR line endings are
// accepted; other encodings must be converted by the host before parsing.
//
// It implements AMLL's default SPL/LRC-family semantics. Timestamps accept one
// to three minute digits, one or two second digits, and an optional one to six
// digit fraction separated by a period or colon. Fractions are truncated to
// milliseconds. Enhanced timestamps may use square or angle brackets; they
// retain absolute track positions in textual order. A terminal timestamp closes
// the row. Repeated leading tags repeat plain rows; enhanced rows use the first
// tag for the line and the final leading tag for the first word.
//
// Metadata names are ASCII letters, normalized to lowercase; repeated values
// are deduplicated. Offset is metadata only and never shifts times. Same-start
// later text and adjacent untimed text become translations. A complete pair of
// ASCII/fullwidth parentheses marks background vocals. Duet is not inferred.
// Missing ends use the next stable-sorted row, including equal starts. The last
// plain row ends at AMLL's 999:59.999 sentinel; a final enhanced row ends one
// second after its final word starts. Options.Duration overrides an inferred
// final end as a host extension.
//
// Source timing is retained explicitly through lyric.DocumentOptions: point,
// reversed, nonmonotonic and out-of-line words are neither removed nor repaired,
// including generated (zero-timed) spaces. Nonnegative reversed line endpoints
// are also retained as data and never describe an active half-open interval.
// Limits, overflow and invalid UTF-8 remain errors. Sorting and malformed tags
// produce diagnostics; Strict promotes diagnostics to errors without changing
// supported source-timing semantics. Parsing performs no resource or media I/O.
package lrc

// Package qrc parses plaintext QRC lines: [start,duration]text(start,duration).
// All times are absolute integer milliseconds, including word starts. Parentheses
// delimit timing tokens and are reserved syntax. Empty payloads are valid;
// nonempty untimed text and trailing text without a timing token are errors.
//
// UTF-8 (optionally BOM-prefixed), LF, CRLF and CR are accepted. [name:value]
// metadata is retained as document properties. [offset:signed-ms] shifts all
// lines and words, with positive values moving later. Repeated offsets warn and
// use the last value. Durations are explicit, so ends are never inferred.
//
// Lines are stably sorted with a warning; equal starts and overlaps are retained.
// Source line identities use qrc:<one-based-source-line> and survive sorting.
// Zero-duration word timing is removed with a warning while line text survives.
// Invalid intervals, malformed tokens and overflows are fatal. Strict mode makes
// warnings fatal. This subset does not interpret AMLL's parenthesized background
// convention; fullwidth parentheses remain ordinary lyric text. Duet,
// translation and romanization are not inferred.
// XML/service containers, compressed and encrypted QRC are unsupported; callers
// must supply plaintext, and the parser never fetches or decodes external data.
package qrc

import (
	"context"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/timedtext"
)

// Parse reads bounded UTF-8 input with cancellation and returns an immutable
// document. Limits and ownership are defined by format.Options and format.Result.
func Parse(ctx context.Context, data []byte, options common.Options) (common.Result, error) {
	return timedtext.Parse(ctx, data, options, false)
}

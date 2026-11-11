// Package qrc parses plaintext QRC lines: [start,duration]text(start,duration).
// All times are absolute integer milliseconds, including word starts. Timing
// tokens are found lazily like AMLL: parenthesized text remains lyric text when a
// later numeric token follows, and unmatched tail text after the final token is
// ignored. Whitespace-only payloads are skipped; a nonempty payload with no
// timing token yields an empty-word line.
//
// UTF-8 (optionally BOM-prefixed), LF, CRLF and CR are accepted. [name:value]
// metadata is retained as document properties. [offset:signed-ms] shifts all
// lines and words, with positive values moving later. Repeated offsets warn and
// use the last value. Durations are explicit, so ends are never inferred.
//
// Lines are stably sorted with a warning; equal starts and overlaps are retained.
// Source line identities use qrc:<one-based-source-line> and survive sorting.
// Source-order point words, decreasing starts and words outside their line are
// retained through the explicit source-timing document policy, including (0,0)
// spaces. Point lines describe empty active intervals. Negative numeric fields,
// overflow and input budgets remain errors. Strict promotes diagnostics to
// errors without dropping supported source timing. ASCII/fullwidth parentheses
// around the first and last timed words mark background lines and are removed,
// matching AMLL. Duet, translation and romanization are not inferred.
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

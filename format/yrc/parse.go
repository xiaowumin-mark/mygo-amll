// Package yrc parses a plaintext subset of YRC. Lines use [start,duration]
// followed by (start,duration,flags)text segments. All times are absolute integer
// milliseconds; word times are not relative to their line. Timing parentheses
// are reserved syntax. Empty payloads are valid; nonempty untimed text is not.
//
// UTF-8 (optionally BOM-prefixed), LF, CRLF and CR are accepted. Metadata tags
// such as [ti:value], [ar:value] and [offset:signed-ms] are retained as document
// properties. The final offset applies to every line and word; positive offsets
// move them later. Repeated offsets warn, with the last value winning. There is
// no end inference because both line and word durations are explicit.
//
// Lines are stably sorted with a warning; equal starts and overlaps are retained.
// Source line identities use yrc:<one-based-source-line> and survive sorting.
// Zero-duration word timing is removed with a warning while preserving line text.
// Other invalid intervals, malformed tokens and time overflows are fatal. Nonzero
// word flags warn because the model does not represent them. Strict mode turns
// warnings into errors. This subset does not interpret AMLL's parenthesized
// background-vocal convention; fullwidth parentheses remain ordinary lyric text.
// Duet, translation and romanization are not inferred. JSON envelopes and other
// service containers are unsupported. Parsing performs no I/O.
package yrc

import (
	"context"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/timedtext"
)

// Parse reads bounded UTF-8 input with cancellation and returns an immutable
// document. Limits and ownership are defined by format.Options and format.Result.
func Parse(ctx context.Context, data []byte, options common.Options) (common.Result, error) {
	return timedtext.Parse(ctx, data, options, true)
}

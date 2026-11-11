// Package yrc parses bounded plaintext YRC using AMLL's lazy token grammar.
// Lines use [start,duration], followed by (start,duration,0)text segments. All
// times are absolute integer milliseconds. Only literal flags 0 form timing
// tokens; other parentheses remain text between valid markers. Empty payloads
// are skipped; a nonempty unmatched payload produces an empty-word line.
//
// UTF-8 (optionally BOM-prefixed), LF, CRLF and CR are accepted. Metadata tags
// such as [ti:value], [ar:value] and [offset:signed-ms] are retained as document
// properties. The final offset applies to every line and word; positive offsets
// move them later. Repeated offsets warn, with the last value winning. There is
// no end inference because both line and word durations are explicit.
//
// Lines are stably sorted with a warning; equal starts and overlaps are retained.
// Source line identities use yrc:<one-based-source-line> and survive sorting.
// Source-order point words, decreasing starts and words outside their line are
// retained through the explicit source-timing document policy. Generated (0,0)
// spaces keep their text and endpoints. Point lines are retained as empty active
// intervals. Time overflow, negative numeric fields and input budgets remain
// errors. Strict turns diagnostics into errors, not supported source timing.
// A complete ASCII/fullwidth parenthesis pair around the first/last word marks
// a background line and is removed. Duet, translation and romanization are not
// inferred. JSON envelopes and other service containers are unsupported.
// Parsing performs no I/O.
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

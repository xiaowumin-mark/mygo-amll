// Package ttml parses an explicitly limited, in-memory TTML lyric profile.
//
// Supported input is UTF-8 TTML (the W3C namespace, its legacy namespace, or
// unqualified elements), with tt/head/metadata/body/div/p/span/br, itunes:key
// (preferred line ID), xml:id/generated ID compatibility fallbacks, xml:lang,
// ttm:agent, inline x-bg/x-translation/x-roman roles, timed ruby, obscene and
// empty-beat word annotations. Background spans become independent overlapping
// lines; nested x-bg roles are rejected, including nesting through ordinary
// spans. Typed head agents determine alternating duet sides in source order:
// groups stay on the main side without changing alternation, a first "other"
// agent starts on the duet side, and changing non-group IDs flips the side.
// Background vocals inherit their main line's side. Agent IDs remain verbatim.
// Missing IDs use v1 only for this alternation rule; v2 has no unconditional
// alternate-side meaning. Both current and legacy iTunes namespaces are read.
//
// Inline annotations and iTunesMetadata translations/transliterations provide
// the first available translation/romanization, including background content.
// Inline values take precedence. Timed sidecar romanization aligns to main
// words by the reference start-time/interval-overlap rules; other languages and
// timed translation arrays are not exposed by this simplified shared model.
// AMLL properties, ttm:title/name, songwriters, root language/timingMode and
// resource-free audio lyricOffset/role attributes are retained as properties.
// Audio properties neither shift lyric timestamps nor cause I/O.
//
// This profile interprets p/span begin/end as absolute song positions. AMLL
// clock expressions and bare seconds round to the nearest millisecond. Explicit
// Apple Word timing accepts bare decimal seconds at nanosecond precision as a
// local extension, as do the h/m/ms suffix forms. body/div timing is ignored as
// in AMLL; p/span time is not shifted by container bounds. Paragraph ranges can
// expand to contain timed child ranges. Source-order lines, words and Ruby keep
// nonnegative endpoints, including point or reversed intervals. Words and Ruby
// can also retain out-of-parent and non-monotonic endpoints through
// lyric.DocumentOptions.PreserveSourceTiming. Reversed word timing produces a
// diagnostic; strict mode rejects it. This policy does not relax the default
// lyric.NewDocument or NewDocumentWithMetadata constructors.
//
// XML namespace bindings, expanded attribute uniqueness and matching QNames
// are validated before lyric conversion. Unbound prefixes are errors and are
// never repaired. Sequential containers, frame/tick clocks, DTDs and external
// entity declarations are unsupported. Unsupported styles, layout and other
// extensions produce diagnostics or errors. External resources are never
// accessed; this package does not claim general TTML, lossless round trips or
// complete Apple Music support.
//
// Missing line ends use the common format normalization policy. dur is relative
// to its element's begin. There is no TTML offset extension in this profile.
// XML whitespace follows the fixed AMLL parser: ECMAScript whitespace is
// collapsed, indentation-only newline text nodes are skipped, and xml:space is
// ignored. A TTML br contributes no lyric text, as in that parser.
// Source and expanded output use the shared format limits; unselected sidecar
// segments still count toward MaxWords. Romanization alignment additionally
// allows at most 32*MaxWords candidate comparisons per parse. Limit failures
// and cancellation return no partial document.
// Diagnostics use one-based physical lines and UTF-8 byte columns. Parse only
// reads the supplied bytes and does not access files, URLs or a network.
package ttml

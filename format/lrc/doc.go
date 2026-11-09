// Package lrc parses UTF-8 LRC and enhanced LRC from caller-provided bytes.
// It performs no I/O. A leading UTF-8 BOM and LF, CRLF, and CR line endings are
// accepted; other encodings must be converted by the host before parsing.
//
// Line timestamps use [minutes:ss], optionally followed by one to three decimal
// digits. Multiple leading timestamps repeat a line, preserving source order
// for equal starts. Enhanced <minutes:ss.fraction> timestamps are absolute track
// positions; a final timestamp with no following text explicitly ends the line.
// Repeated enhanced lines shift their word times by each line timestamp's delta.
//
// Metadata [name:value] is retained in the document. The last [offset:signed-ms]
// shifts every line and word; duplicate offsets produce a diagnostic. Missing
// line ends use the next distinct start, or Options.Duration for the last group,
// or a five-second fallback. Normalization reports inferred ends, reordered
// lines, and dropped zero-duration word timings. Equal-start lines remain
// separate; translations, duet roles, and background vocals are not inferred.
// AMLL-specific metadata extensions and timestamp precision above milliseconds
// are outside this subset. Enhanced timestamps must be nondecreasing. Strict
// mode rejects every diagnostic.
package lrc

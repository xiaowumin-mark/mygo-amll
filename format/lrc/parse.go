package lrc

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Parse validates and normalizes an in-memory LRC document. The returned document
// owns its data and is safe to share; data may be reused after Parse returns.
// Cancellation and resource-limit failures never return a partial document.
func Parse(ctx context.Context, data []byte, options common.Options) (common.Result, error) {
	p, err := formatutil.New(ctx, data, options)
	if err != nil {
		return common.Result{}, err
	}
	source := string(data)
	bomBytes := len(source) - len(strings.TrimPrefix(source, "\ufeff"))
	source = source[bomBytes:]
	var lines []formatutil.Line
	var metadata []lyric.Property
	var offset time.Duration
	offsetSeen := false
	lineNumber := 0
	for len(source) > 0 {
		lineNumber++
		columnBase := 0
		if lineNumber == 1 {
			columnBase = bomBytes
		}
		if err := p.Check(); err != nil {
			return failed(p, err)
		}
		end := strings.IndexAny(source, "\r\n")
		var raw string
		if end < 0 {
			raw, source = source, ""
		} else {
			raw = source[:end]
			next := end + 1
			if source[end] == '\r' && next < len(source) && source[next] == '\n' {
				next++
			}
			source = source[next:]
		}
		prefix := len(raw) - len(strings.TrimLeft(raw, " \t"))
		column := columnBase + prefix + 1
		text := raw[prefix:]
		if strings.TrimSpace(text) == "" {
			continue
		}
		if text[0] != '[' {
			if err := p.Warn(lineNumber, column, "untimed-line", "ignored text without a leading LRC timestamp"); err != nil {
				return failed(p, err)
			}
			continue
		}
		close := strings.IndexByte(text, ']')
		if close < 0 {
			if err := p.Warn(lineNumber, column, "invalid-tag", "ignored an unterminated LRC tag"); err != nil {
				return failed(p, err)
			}
			continue
		}
		first := text[1:close]
		start, timeErr := timestamp(first)
		if timeErr != nil {
			key, value, ok := strings.Cut(first, ":")
			if ok && metadataName(key) {
				if strings.TrimSpace(text[close+1:]) != "" {
					if err := p.Warn(lineNumber, column+close+1, "metadata-tail", "ignored text following a metadata tag"); err != nil {
						return failed(p, err)
					}
				}
				metadata = append(metadata, lyric.Property{Name: key, Value: value})
				if strings.EqualFold(key, "offset") {
					ms, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
					if err != nil || ms > math.MaxInt64/int64(time.Millisecond) || ms < math.MinInt64/int64(time.Millisecond) {
						return failed(p, formatutil.Error(lineNumber, column, "offset", "invalid millisecond offset"))
					}
					if offsetSeen {
						if err := p.Warn(lineNumber, column, "duplicate-offset", "the last offset tag replaces the previous offset"); err != nil {
							return failed(p, err)
						}
					}
					offset, offsetSeen = time.Duration(ms)*time.Millisecond, true
				}
				continue
			}
			if err := p.Warn(lineNumber, column, "invalid-timestamp", "ignored line: "+timeErr.Error()); err != nil {
				return failed(p, err)
			}
			continue
		}
		starts := []time.Duration{start}
		used := close + 1
		for used < len(text) && text[used] == '[' {
			close := strings.IndexByte(text[used:], ']')
			if close < 0 {
				break
			}
			value, err := timestamp(text[used+1 : used+close])
			if err != nil {
				break // A bracket in lyric text is not another timestamp.
			}
			if len(starts) >= p.Options.Limits.MaxLines {
				return failed(p, formatutil.Error(lineNumber, column+used, "limit", "repeated timestamps exceed line limit"))
			}
			starts = append(starts, value)
			used += close + 1
		}
		plain, words, explicitEnd, err := enhanced(p, text[used:], lineNumber, column+used)
		if err != nil {
			return failed(p, err)
		}
		for tagIndex, lineStart := range starts {
			if err := p.AddLine(); err != nil {
				return failed(p, err)
			}
			if err := p.AddWords(len(words)); err != nil {
				return failed(p, err)
			}
			line := lyric.Line{ID: fmt.Sprintf("lrc:%d:%d", lineNumber, tagIndex), Text: plain, Start: lineStart}
			delta := lineStart - starts[0]
			if explicitEnd >= 0 {
				line.End, err = shift(explicitEnd, delta)
				if err != nil {
					return failed(p, formatutil.Error(lineNumber, column, "time", err.Error()))
				}
			}
			if len(words) > 0 {
				line.Words = make([]lyric.Word, len(words))
				for i, word := range words {
					line.Words[i] = word
					line.Words[i].Start, err = shift(word.Start, delta)
					if err != nil {
						return failed(p, formatutil.Error(lineNumber, column, "time", fmt.Sprintf("word %d: %v", i, err)))
					}
					if word.End >= 0 {
						line.Words[i].End, err = shift(word.End, delta)
						if err != nil {
							return failed(p, formatutil.Error(lineNumber, column, "time", fmt.Sprintf("word %d: %v", i, err)))
						}
					}
				}
			}
			lines = append(lines, formatutil.Line{Line: line, SourceLine: lineNumber, InferEnd: explicitEnd < 0})
		}
	}
	return p.Finish(lines, metadata, offset)
}

func failed(p *formatutil.Parser, err error) (common.Result, error) {
	return common.Result{Diagnostics: p.Diagnostics}, err
}

func metadataName(key string) bool {
	if key == "" {
		return false
	}
	for _, c := range key {
		if c < 'A' || c > 'Z' && c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

func timestamp(value string) (time.Duration, error) {
	minutes, rest, ok := strings.Cut(value, ":")
	if !ok || len(minutes) == 0 || len(rest) < 2 {
		return 0, fmt.Errorf("expected minutes:ss with optional millisecond fraction")
	}
	for _, c := range minutes {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("timestamp minutes must be nonnegative digits")
		}
	}
	if rest[0] < '0' || rest[0] > '9' || rest[1] < '0' || rest[1] > '9' {
		return 0, fmt.Errorf("timestamp seconds require two digits")
	}
	seconds := int64(rest[0]-'0')*10 + int64(rest[1]-'0')
	if seconds >= 60 {
		return 0, fmt.Errorf("timestamp seconds must be below 60")
	}
	var fraction int64
	if len(rest) > 2 {
		if rest[2] != '.' || len(rest) < 4 || len(rest) > 6 {
			return 0, fmt.Errorf("timestamp fraction requires one to three decimal digits")
		}
		for _, c := range rest[3:] {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("timestamp fraction must contain decimal digits")
			}
			fraction = fraction*10 + int64(c-'0')
		}
		for i := len(rest) - 3; i < 3; i++ {
			fraction *= 10
		}
	}
	min, err := strconv.ParseInt(minutes, 10, 64)
	remaining := seconds*int64(time.Second) + fraction*int64(time.Millisecond)
	if err != nil || min > (math.MaxInt64-remaining)/int64(time.Minute) {
		return 0, fmt.Errorf("timestamp exceeds time.Duration range")
	}
	return time.Duration(min*int64(time.Minute) + remaining), nil
}

func shift(value, delta time.Duration) (time.Duration, error) {
	if delta > 0 && value > time.Duration(math.MaxInt64)-delta || delta < 0 && value < -delta {
		return 0, fmt.Errorf("repeated timestamp shifts timing outside the supported range")
	}
	return value + delta, nil
}

// enhanced treats malformed angle tags as plain text with a diagnostic rather
// than silently deleting content. Only recognized timestamp tags are removed.
func enhanced(p *formatutil.Parser, payload string, sourceLine, column int) (string, []lyric.Word, time.Duration, error) {
	first := strings.IndexByte(payload, '<')
	if first < 0 {
		return payload, nil, -1, nil
	}
	var text strings.Builder
	text.WriteString(payload[:first])
	var words []lyric.Word
	lastEnd := time.Duration(-1)
	previous := time.Duration(-1)
	remaining, consumed := payload[first:], first
	for len(remaining) > 0 {
		if err := p.Check(); err != nil {
			return "", nil, 0, err
		}
		close := strings.IndexByte(remaining, '>')
		if close < 0 {
			if err := p.Warn(sourceLine, column+consumed, "invalid-word-timestamp", "unclosed enhanced timestamp retained as plain text"); err != nil {
				return "", nil, 0, err
			}
			return payload, nil, -1, nil
		}
		start, err := timestamp(remaining[1:close])
		if err != nil {
			if err := p.Warn(sourceLine, column+consumed, "invalid-word-timestamp", "malformed enhanced timestamps retained as plain text"); err != nil {
				return "", nil, 0, err
			}
			return payload, nil, -1, nil
		}
		if start < previous {
			return "", nil, 0, formatutil.Error(sourceLine, column+consumed, "time", "enhanced timestamps must be nondecreasing")
		}
		previous = start
		if len(words) > 0 && words[len(words)-1].End < 0 {
			words[len(words)-1].End = start
		}
		after := remaining[close+1:]
		next := strings.IndexByte(after, '<')
		if next < 0 {
			next = len(after)
		}
		segment := after[:next]
		if segment == "" && next == len(after) {
			lastEnd = start
		} else if segment != "" {
			if len(words) >= p.Options.Limits.MaxWords {
				return "", nil, 0, formatutil.Error(sourceLine, column+consumed, "limit", "enhanced words exceed word limit")
			}
			words = append(words, lyric.Word{Text: segment, Start: start, End: -1})
			text.WriteString(segment)
		}
		consumed += close + 1 + next
		remaining = after[next:]
	}
	if first > 0 {
		if err := p.Warn(sourceLine, column, "untimed-prefix", "text before the first enhanced timestamp has no word timing"); err != nil {
			return "", nil, 0, err
		}
	}
	return text.String(), words, lastEnd, nil
}

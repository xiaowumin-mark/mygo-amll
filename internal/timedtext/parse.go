// Package timedtext implements the shared plaintext millisecond grammar of YRC
// and QRC. It does not decode containers, compression or encryption.
package timedtext

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// Parse accepts YRC when prefixTiming is true, otherwise QRC.
func Parse(ctx context.Context, data []byte, options common.Options, prefixTiming bool) (result common.Result, err error) {
	p, err := formatutil.New(ctx, data, options)
	if err != nil {
		return result, err
	}
	text := string(data)
	bomOffset := 0
	if strings.HasPrefix(text, "\ufeff") {
		text = text[3:]
		bomOffset = 3
	}
	defer func() {
		result.Diagnostics = p.Diagnostics
		// Token columns were measured after removing the BOM; the public contract
		// counts original UTF-8 bytes. Validation errors above already use data.
		if bomOffset == 0 {
			return
		}
		for i := range result.Diagnostics {
			if result.Diagnostics[i].Line == 1 {
				result.Diagnostics[i].Column += bomOffset
			}
		}
		var located *common.ParseError
		if errors.As(err, &located) && located.Line == 1 {
			adjusted := *located
			adjusted.Column += bomOffset
			err = &adjusted
		}
	}()
	var lines []formatutil.Line
	var metadata []lyric.Property
	var offset time.Duration
	seenOffset := false
	for sourceLine, begin := 1, 0; begin < len(text); sourceLine++ {
		if err := p.Check(); err != nil {
			return result, err
		}
		end := begin
		for end < len(text) && text[end] != '\r' && text[end] != '\n' {
			end++
		}
		raw := text[begin:end]
		begin = end
		if begin < len(text) {
			begin++
			if text[end] == '\r' && begin < len(text) && text[begin] == '\n' {
				begin++
			}
		}
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if raw[0] != '[' {
			return result, formatutil.Error(sourceLine, 1, "unsupported", "expected plaintext [start,duration] lyrics; containers are unsupported")
		}
		close := strings.IndexByte(raw, ']')
		if close < 0 {
			return result, formatutil.Error(sourceLine, 1, "syntax", "unclosed line header")
		}
		header := raw[1:close]
		if name, value, ok := strings.Cut(header, ":"); ok {
			if !metadataName(name) || strings.TrimSpace(raw[close+1:]) != "" {
				return result, formatutil.Error(sourceLine, 1, "syntax", "invalid metadata tag")
			}
			name = strings.ToLower(name)
			metadata = append(metadata, lyric.Property{Name: name, Value: value})
			if name == "offset" {
				parsed, err := milliseconds(value, true)
				if err != nil {
					return result, formatutil.Error(sourceLine, 2+len(name)+1, "time", "invalid millisecond offset")
				}
				if seenOffset {
					if err := p.Warn(sourceLine, 1, "duplicate-offset", "last offset tag overrides earlier offsets"); err != nil {
						return result, err
					}
				}
				offset, seenOffset = parsed, true
			}
			continue
		}
		start, duration, _, err := timing(header, false)
		if err != nil {
			return result, formatutil.Error(sourceLine, 2, "time", "line header requires nonnegative millisecond start,duration")
		}
		lineEnd, err := formatutil.Shift(start, duration)
		if err != nil {
			return result, formatutil.Error(sourceLine, 2, "time", "line end overflows time.Duration")
		}
		if err := p.AddLine(); err != nil {
			return result, err
		}
		kind := "qrc"
		if prefixTiming {
			kind = "yrc"
		}
		line := formatutil.Line{Line: lyric.Line{ID: kind + ":" + strconv.Itoa(sourceLine), Start: start, End: lineEnd}, SourceLine: sourceLine}
		payload := raw[close+1:]
		var plain strings.Builder
		for position := 0; position < len(payload); {
			if err := p.Check(); err != nil {
				return result, err
			}
			wordStart := position
			open := position
			if !prefixTiming {
				if next := strings.IndexByte(payload[position:], '('); next >= 0 {
					open += next
				} else {
					return result, formatutil.Error(sourceLine, close+2+position, "syntax", "QRC word text requires a trailing (start,duration) timing")
				}
			}
			if payload[open] != '(' {
				return result, formatutil.Error(sourceLine, close+2+open, "syntax", "YRC word requires a leading (start,duration,flags) timing")
			}
			wordClose := strings.IndexByte(payload[open+1:], ')')
			if wordClose < 0 {
				return result, formatutil.Error(sourceLine, close+2+open, "syntax", "unclosed word timing")
			}
			wordClose += open + 1
			start, duration, flags, err := timing(payload[open+1:wordClose], prefixTiming)
			if err != nil {
				return result, formatutil.Error(sourceLine, close+3+open, "time", "invalid millisecond word timing")
			}
			end, err := formatutil.Shift(start, duration)
			if err != nil {
				return result, formatutil.Error(sourceLine, close+3+open, "time", "word end overflows time.Duration")
			}
			if prefixTiming && flags != 0 {
				if err := p.Warn(sourceLine, close+3+open, "word-flags", "nonzero YRC word flags are not represented"); err != nil {
					return result, err
				}
			}
			wordText := payload[wordStart:open]
			position = wordClose + 1
			if prefixTiming {
				textEnd := len(payload)
				if next := strings.IndexByte(payload[position:], '('); next >= 0 {
					textEnd = position + next
				}
				wordText = payload[position:textEnd]
				position = textEnd
			}
			if err := p.AddWords(1); err != nil {
				return result, err
			}
			line.Words = append(line.Words, lyric.Word{Text: wordText, Start: start, End: end})
			plain.WriteString(wordText)
		}
		line.Text = plain.String()
		lines = append(lines, line)
	}
	return p.Finish(lines, metadata, offset)
}

func metadataName(value string) bool {
	if value == "" || !(value[0] >= 'A' && value[0] <= 'Z' || value[0] >= 'a' && value[0] <= 'z') {
		return false
	}
	for _, char := range value {
		if !(char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func milliseconds(value string, signed bool) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if !signed && (strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+")) {
		return 0, strconv.ErrSyntax
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil || number > math.MaxInt64/int64(time.Millisecond) || number < math.MinInt64/int64(time.Millisecond) {
		return 0, strconv.ErrRange
	}
	return time.Duration(number) * time.Millisecond, nil
}

func timing(value string, flagsRequired bool) (time.Duration, time.Duration, uint64, error) {
	first, rest, ok := strings.Cut(value, ",")
	if !ok {
		return 0, 0, 0, strconv.ErrSyntax
	}
	second := rest
	var flags uint64
	if flagsRequired {
		var rawFlags string
		second, rawFlags, ok = strings.Cut(rest, ",")
		if !ok {
			return 0, 0, 0, strconv.ErrSyntax
		}
		var err error
		flags, err = strconv.ParseUint(strings.TrimSpace(rawFlags), 10, 64)
		if err != nil {
			return 0, 0, 0, err
		}
	}
	start, err := milliseconds(first, false)
	if err != nil {
		return 0, 0, 0, err
	}
	duration, err := milliseconds(second, false)
	return start, duration, flags, err
}

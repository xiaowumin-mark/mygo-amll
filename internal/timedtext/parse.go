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
		trimmed := strings.TrimLeftFunc(raw, isQRCWhitespace)
		lineColumnOffset := len(raw) - len(trimmed)
		raw = strings.TrimRightFunc(trimmed, isQRCWhitespace)
		if raw == "" {
			continue
		}
		if raw[0] != '[' {
			return result, formatutil.Error(sourceLine, lineColumnOffset+1, "unsupported", "expected plaintext [start,duration] lyrics; containers are unsupported")
		}
		close := strings.IndexByte(raw, ']')
		if close < 0 {
			return result, formatutil.Error(sourceLine, lineColumnOffset+1, "syntax", "unclosed line header")
		}
		header := raw[1:close]
		if name, value, ok := strings.Cut(header, ":"); ok {
			if !metadataName(name) || strings.TrimSpace(raw[close+1:]) != "" {
				return result, formatutil.Error(sourceLine, lineColumnOffset+1, "syntax", "invalid metadata tag")
			}
			name = strings.ToLower(name)
			metadata = append(metadata, lyric.Property{Name: name, Value: value})
			if name == "offset" {
				parsed, err := milliseconds(value, true)
				if err != nil {
					return result, formatutil.Error(sourceLine, lineColumnOffset+2+len(name)+1, "time", "invalid millisecond offset")
				}
				if seenOffset {
					if err := p.Warn(sourceLine, lineColumnOffset+1, "duplicate-offset", "last offset tag overrides earlier offsets"); err != nil {
						return result, err
					}
				}
				offset, seenOffset = parsed, true
			}
			continue
		}
		start, duration, _, err := timing(header, false)
		if err != nil {
			return result, formatutil.Error(sourceLine, lineColumnOffset+2, "time", "line header requires nonnegative millisecond start,duration")
		}
		lineEnd, err := formatutil.Shift(start, duration)
		if err != nil {
			return result, formatutil.Error(sourceLine, lineColumnOffset+2, "time", "line end overflows time.Duration")
		}
		payload := raw[close+1:]
		trimmed = strings.TrimLeftFunc(payload, isQRCWhitespace)
		payloadColumnOffset := len(payload) - len(trimmed)
		payload = strings.TrimRightFunc(trimmed, isQRCWhitespace)
		if payload == "" {
			continue
		}
		if err := p.AddLine(); err != nil {
			return result, err
		}
		kind := "qrc"
		if prefixTiming {
			kind = "yrc"
		}
		// Generated lyrics use source-order (0,0) segments for spaces. Retain
		// those authored endpoints and text even when they precede nearby words.
		line := formatutil.Line{Line: lyric.Line{ID: kind + ":" + strconv.Itoa(sourceLine), Start: start, End: lineEnd}, SourceLine: sourceLine, PreserveTiming: true}
		var plain strings.Builder
		if prefixTiming {
			if err := parseYRCWords(p, payload, &line, &plain, sourceLine, lineColumnOffset+payloadColumnOffset+close+2); err != nil {
				return result, err
			}
		} else {
			for position := 0; position < len(payload); {
				if err := p.Check(); err != nil {
					return result, err
				}
				wordStart := position
				open := position
				wordClose := -1
				var found bool
				open, wordClose, found = nextQRCTiming(payload, position)
				if !found {
					// AMLL's global lazy regexp stops when no complete numeric token
					// remains. Any unmatched tail after the last timed word is ignored.
					break
				}
				start, duration, _, err := timing(payload[open+1:wordClose], false)
				if err != nil {
					return result, formatutil.Error(sourceLine, lineColumnOffset+payloadColumnOffset+close+3+open, "time", "invalid millisecond word timing")
				}
				end, err := formatutil.Shift(start, duration)
				if err != nil {
					return result, formatutil.Error(sourceLine, lineColumnOffset+payloadColumnOffset+close+3+open, "time", "word end overflows time.Duration")
				}
				wordText := payload[wordStart:open]
				position = wordClose + 1
				if err := p.AddWords(1); err != nil {
					return result, err
				}
				line.Words = append(line.Words, lyric.Word{Text: wordText, Start: start, End: end})
				plain.WriteString(wordText)
			}
		}
		if len(line.Words) > 0 && timedBackground(line.Words) {
			line.Background = true
			line.Words[0].Text = strings.TrimPrefix(strings.TrimPrefix(line.Words[0].Text, "("), "（")
			last := len(line.Words) - 1
			line.Words[last].Text = strings.TrimSuffix(strings.TrimSuffix(line.Words[last].Text, ")"), "）")
			plain.Reset()
			for _, word := range line.Words {
				plain.WriteString(word.Text)
			}
		}
		line.Text = plain.String()
		lines = append(lines, line)
	}
	return p.Finish(lines, metadata, offset)
}

// nextQRCTiming searches like AMLL's global QRC regexp: ordinary or malformed
// parenthesized fragments remain lyric text when a later numeric timing token
// follows them. The caller retains all bytes before the returned token as the
// timed word's text. Digit-only overflowing tokens are still matched here and
// rejected by timing, preserving this parser's overflow protection.
func nextQRCTiming(payload string, from int) (open, close int, found bool) {
	for search := from; search < len(payload); {
		next := strings.IndexByte(payload[search:], '(')
		if next < 0 {
			return 0, 0, false
		}
		open = search + next
		end := strings.IndexByte(payload[open+1:], ')')
		if end < 0 {
			search = open + 1
			continue
		}
		close = open + 1 + end
		if qrcNumericTiming(payload[open+1 : close]) {
			return open, close, true
		}
		search = open + 1
	}
	return 0, 0, false
}

// parseYRCWords follows AMLL's anchored lazy YRC expression. Each search skips
// arbitrary lyric text until the next complete (start,duration,0) marker. Text
// between markers belongs to the preceding marker; text before the first marker
// is ignored, and an untimed tail extends the final word.
func parseYRCWords(p *formatutil.Parser, payload string, line *formatutil.Line, plain *strings.Builder, sourceLine, baseColumn int) error {
	type pendingWord struct {
		start time.Duration
		end   time.Duration
	}
	var pending *pendingWord
	textStart := 0
	appendWord := func(text string, word pendingWord) error {
		if text == "" {
			return nil
		}
		if err := p.AddWords(1); err != nil {
			return err
		}
		line.Words = append(line.Words, lyric.Word{Text: text, Start: word.start, End: word.end})
		plain.WriteString(text)
		return nil
	}

	for position := 0; position < len(payload); {
		if err := p.Check(); err != nil {
			return err
		}
		open, close, found := nextYRCTiming(payload, position)
		if !found {
			if pending != nil {
				return appendWord(payload[textStart:], *pending)
			}
			return nil
		}
		if pending != nil {
			if err := appendWord(payload[textStart:open], *pending); err != nil {
				return err
			}
		}
		start, duration, _, err := timing(payload[open+1:close], true)
		if err != nil {
			return formatutil.Error(sourceLine, baseColumn+open+1, "time", "invalid millisecond word timing")
		}
		end, err := formatutil.Shift(start, duration)
		if err != nil {
			return formatutil.Error(sourceLine, baseColumn+open+1, "time", "word end overflows time.Duration")
		}
		pending = &pendingWord{start: start, end: end}
		textStart = close + 1
		position = textStart
	}
	if pending != nil {
		return appendWord(payload[textStart:], *pending)
	}
	return nil
}

// nextYRCTiming matches only literal flags 0, as AMLL's YRC expression does.
// Other parenthesized fragments remain part of the lazily captured lyric text.
func nextYRCTiming(payload string, from int) (open, close int, found bool) {
	for search := from; search < len(payload); {
		next := strings.IndexByte(payload[search:], '(')
		if next < 0 {
			return 0, 0, false
		}
		open = search + next
		index := open + 1
		startBegin := index
		for index < len(payload) && payload[index] >= '0' && payload[index] <= '9' {
			index++
		}
		if index == startBegin || index >= len(payload) || payload[index] != ',' {
			search = open + 1
			continue
		}
		index++
		durationBegin := index
		for index < len(payload) && payload[index] >= '0' && payload[index] <= '9' {
			index++
		}
		if index == durationBegin || index+2 >= len(payload) || payload[index] != ',' || payload[index+1] != '0' || payload[index+2] != ')' {
			search = open + 1
			continue
		}
		return open, index + 2, true
	}
	return 0, 0, false
}

func qrcNumericTiming(value string) bool {
	first, rest, ok := strings.Cut(value, ",")
	if !ok || first == "" || rest == "" || strings.Contains(rest, ",") {
		return false
	}
	for _, field := range []string{first, rest} {
		for _, digit := range field {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return true
}

func timedBackground(words []lyric.Word) bool {
	first, last := words[0].Text, words[len(words)-1].Text
	return (strings.HasPrefix(first, "(") || strings.HasPrefix(first, "（")) &&
		(strings.HasSuffix(last, ")") || strings.HasSuffix(last, "）"))
}

// isQRCWhitespace follows JavaScript String.trim's edge set. In particular,
// it includes BOM and excludes U+0085, unlike unicode.IsSpace.
func isQRCWhitespace(r rune) bool {
	return r >= '\t' && r <= '\r' || r == ' ' || r == '\u00a0' || r == '\u1680' ||
		r >= '\u2000' && r <= '\u200a' || r == '\u2028' || r == '\u2029' ||
		r == '\u202f' || r == '\u205f' || r == '\u3000' || r == '\ufeff'
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

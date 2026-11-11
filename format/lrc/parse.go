// Package lrc parses the AMLL-default LRC/SPL timestamp language into the
// repository's bounded immutable lyric model.
package lrc

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

type token struct {
	timed  bool
	value  time.Duration
	text   string
	column int
}

type metadataEntry struct {
	name   string
	values []string
	seen   map[string]struct{}
}

func failed(p *formatutil.Parser, err error) (common.Result, error) {
	return common.Result{Diagnostics: p.Diagnostics}, err
}

// Parse validates and normalizes an in-memory LRC document. The returned
// document owns its data and is safe to share; data may be reused after Parse
// returns. Word and line timestamps are absolute. The AMLL offset tag is
// metadata only and does not move lyric timestamps.
func Parse(ctx context.Context, data []byte, options common.Options) (common.Result, error) {
	p, err := formatutil.New(ctx, data, options)
	if err != nil {
		return common.Result{}, err
	}
	source := string(data)
	bomBytes := len(source) - len(strings.TrimPrefix(source, "\ufeff"))
	source = source[bomBytes:]
	var lines []formatutil.Line
	var metadata []metadataEntry
	metadataIndex := make(map[string]int)
	mainByStart := make(map[time.Duration]int)
	translations := translationState{parser: p}
	var lastMain []int
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
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		column := columnBase + len(raw) - len(strings.TrimLeftFunc(raw, unicode.IsSpace)) + 1
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed, "]") {
			if err := p.Warn(lineNumber, column, "invalid-tag", "ignored an unterminated LRC tag"); err != nil {
				return failed(p, err)
			}
			continue
		}

		if key, value, ok := parseMetadataLine(trimmed); ok {
			if value == "" {
				continue
			}
			index, found := metadataIndex[key]
			if !found {
				index = len(metadata)
				metadataIndex[key] = index
				metadata = append(metadata, metadataEntry{name: key, seen: make(map[string]struct{})})
			}
			entry := &metadata[index]
			if _, exists := entry.seen[value]; !exists {
				entry.seen[value] = struct{}{}
				entry.values = append(entry.values, value)
			}
			continue
		}
		// A malformed metadata-looking line is ignored with a warning, matching
		// the previous bounded parser's useful source diagnostic.
		if strings.HasPrefix(trimmed, "[") {
			close := strings.IndexByte(trimmed, ']')
			if close < 0 {
				if err := p.Warn(lineNumber, column, "invalid-tag", "ignored an unterminated LRC tag"); err != nil {
					return failed(p, err)
				}
				continue
			}
			if !metadataCandidate(trimmed[1:close]) {
				if _, timeErr := timestamp(trimmed[1:close]); timeErr != nil {
					if err := p.Warn(lineNumber, column, "invalid-timestamp", "ignored line: "+timeErr.Error()); err != nil {
						return failed(p, err)
					}
					continue
				}
			}
			if metadataCandidate(trimmed[1:close]) {
				if err := p.Warn(lineNumber, column+close+1, "metadata-tail", "ignored malformed or nonempty metadata tag"); err != nil {
					return failed(p, err)
				}
				continue
			}
		}

		tokens, err := tokenize(p, trimmed, lineNumber, column)
		if err != nil {
			return failed(p, err)
		}
		firstText := len(tokens)
		var plain strings.Builder
		for i, part := range tokens {
			if part.timed {
				continue
			}
			if firstText == len(tokens) {
				firstText = i
			}
			plain.WriteString(part.text)
		}
		text := plain.String()
		if strings.TrimSpace(text) == "" {
			// AMLL interprets a timestamp-only row as the explicit end of the
			// last line in the immediately preceding main-line group.
			if len(lastMain) != 0 {
				lastToken := lastTimedToken(tokens)
				if lastToken != nil {
					index := lastMain[len(lastMain)-1]
					line := &lines[index]
					if line.InferEnd && lastToken.value > line.Start {
						line.End, line.InferEnd = lastToken.value, false
					}
				}
			}
			continue
		}

		leading := leadingTimeTokens(tokens, firstText)
		baseTimes := tokenTimes(leading)
		background := isBackground(text)
		wordSync := firstText > 0 && hasTimedTokenAfter(tokens, firstText)
		if wordSync {
			baseTag := firstTimedToken(tokens)
			previousTime := tokenBeforeText(tokens, firstText)
			if baseTag == nil || previousTime == nil {
				continue
			}
			words, explicitEnd, err := enhancedWords(p, tokens, firstText, previousTime.value, lineNumber, column)
			if err != nil {
				return failed(p, err)
			}
			if !background {
				attached, err := translations.attach(text, []time.Duration{baseTag.value}, lastMain, mainByStart, lines, lineNumber, column)
				if err != nil {
					return failed(p, err)
				}
				if attached {
					continue
				}
			}
			if len(words) == 0 {
				if !background {
					if _, err := translations.attach(text, []time.Duration{baseTag.value}, lastMain, mainByStart, lines, lineNumber, column); err != nil {
						return failed(p, err)
					}
				}
				continue
			}
			if len(words) == 1 && words[0].Start == baseTag.value {
				words[0] = lyric.Word{Text: text, Start: baseTag.value, End: -1}
			}
			if background {
				trimBackgroundWords(words)
				text = joinWords(words)
			}
			line := lyric.Line{ID: fmt.Sprintf("lrc:%d:0", lineNumber), Text: text, Start: baseTag.value, Words: words, Background: background}
			if explicitEnd >= 0 {
				line.End = explicitEnd
			}
			index, err := appendLine(p, &lines, line, lineNumber, baseTag.column, explicitEnd < 0)
			if err != nil {
				return failed(p, err)
			}
			lastMain = []int{index}
			if _, exists := mainByStart[baseTag.value]; !exists {
				mainByStart[baseTag.value] = index
			}
			continue
		}

		// Plain LRC and repeated-start rows take every leading start tag. AMLL
		// treats repeated occurrences of the same start as one row.
		if !background {
			attached, err := translations.attach(text, baseTimes, lastMain, mainByStart, lines, lineNumber, column)
			if err != nil {
				return failed(p, err)
			}
			if attached {
				continue
			}
		}
		baseTimes = uniqueTimes(baseTimes)
		if len(baseTimes) == 0 {
			// Untimed text is a translation only when it attached above; AMLL
			// otherwise discards it without manufacturing a timed lyric line.
			if err := p.Warn(lineNumber, column, "untimed-line", "ignored text without a leading LRC timestamp or translation target"); err != nil {
				return failed(p, err)
			}
			continue
		}
		if background {
			text = trimBackgroundText(text)
		}
		lastMain = lastMain[:0]
		for tagIndex, start := range baseTimes {
			line := lyric.Line{
				ID: fmt.Sprintf("lrc:%d:%d", lineNumber, tagIndex), Text: text,
				Start: start, Background: background,
				Words: []lyric.Word{{Text: text, Start: start, End: -1}},
			}
			tagColumn := leadingTagColumn(leading, start, column)
			index, err := appendLine(p, &lines, line, lineNumber, tagColumn, true)
			if err != nil {
				return failed(p, err)
			}
			lastMain = append(lastMain, index)
			if _, exists := mainByStart[start]; !exists {
				mainByStart[start] = index
			}
		}
	}
	if err := resolveEndTimes(p, lines); err != nil {
		return failed(p, err)
	}
	properties := make([]lyric.Property, 0, len(metadata))
	for _, entry := range metadata {
		for _, value := range entry.values {
			properties = append(properties, lyric.Property{Name: entry.name, Value: value})
		}
	}
	return p.Finish(lines, properties, 0)
}

// resolveEndTimes mirrors AMLL's source-order word endpoints and stable line
// ordering. Equal-start rows use the very next row's start, so a point interval
// remains a point instead of being silently extended to a later distinct row.
func resolveEndTimes(p *formatutil.Parser, lines []formatutil.Line) error {
	if !sort.SliceIsSorted(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start }) {
		if err := p.Warn(1, 1, "sorted", "lines stably sorted by start time"); err != nil {
			return err
		}
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].Start < lines[j].Start })
	}
	for index := range lines {
		if err := p.Check(); err != nil {
			return err
		}
		line := &lines[index]
		lastLine := index == len(lines)-1
		if line.InferEnd {
			switch {
			case !lastLine:
				line.End = lines[index+1].Start
			case p.Options.Duration > 0:
				line.End = p.Options.Duration
			case !wordSynced(line.Line):
				line.End = 999*time.Minute + 59_999*time.Millisecond
			default:
				lastWord := line.Words[len(line.Words)-1]
				end, err := formatutil.Shift(lastWord.Start, time.Second)
				if err != nil {
					return formatutil.Error(line.SourceLine, 1, "time", err.Error())
				}
				line.End = end
			}
			line.InferEnd = false
		}
		for wordIndex := range line.Words {
			word := &line.Words[wordIndex]
			if word.End < 0 {
				word.End = line.End
				if wordIndex+1 < len(line.Words) {
					word.End = line.Words[wordIndex+1].Start
				}
			}
		}
	}
	return nil
}

func appendLine(p *formatutil.Parser, lines *[]formatutil.Line, line lyric.Line, sourceLine, column int, inferEnd bool) (int, error) {
	if len(*lines) >= p.Options.Limits.MaxLines {
		return 0, formatutil.Error(sourceLine, column, "limit", "repeated timestamps exceed line limit")
	}
	if err := p.AddLine(); err != nil {
		return 0, err
	}
	if err := p.AddWords(len(line.Words)); err != nil {
		return 0, err
	}
	index := len(*lines)
	*lines = append(*lines, formatutil.Line{Line: line, SourceLine: sourceLine, InferEnd: inferEnd, PreserveTiming: true})
	return index, nil
}

func wordSynced(line lyric.Line) bool {
	if len(line.Words) == 0 {
		return false
	}
	return len(line.Words) > 1 || line.Words[0].Start != line.Start
}

func tokenize(p *formatutil.Parser, line string, sourceLine, column int) ([]token, error) {
	var tokens []token
	for position := 0; position < len(line); {
		if err := p.Check(); err != nil {
			return nil, err
		}
		openAt := position
		for openAt < len(line) && line[openAt] != '[' && line[openAt] != '<' {
			openAt++
		}
		if openAt > position {
			tokens = append(tokens, token{text: line[position:openAt]})
			position = openAt
		}
		if position == len(line) {
			break
		}
		// AMLL's LRC family expression accepts either closing delimiter for
		// either opener: (?:\[|<)...(?:\]|>). Keep this permissive pairing.
		closeAt := strings.IndexAny(line[position+1:], "]>")
		if closeAt < 0 {
			tokens = append(tokens, token{text: line[position:]})
			if line[position] == '<' {
				if err := p.Warn(sourceLine, column+position, "invalid-word-timestamp", "unclosed enhanced timestamp retained as plain text"); err != nil {
					return nil, err
				}
			}
			break
		}
		closeAt += position + 1
		value, err := timestamp(line[position+1 : closeAt])
		if err != nil {
			// Non-time brackets are ordinary lyric text. Angle brackets that
			// resemble broken enhanced tags retain the existing useful warning.
			// Advance only past this opener. The source regex is unanchored, so a
			// later opener inside malformed bracket text can still start a valid
			// timestamp token (for example "<bad<00:02>").
			if line[position] == '<' {
				if warnErr := p.Warn(sourceLine, column+position, "invalid-word-timestamp", "malformed enhanced timestamps retained as plain text"); warnErr != nil {
					return nil, warnErr
				}
			}
			tokens = append(tokens, token{text: line[position : position+1]})
			position++
			continue
		}
		tokens = append(tokens, token{timed: true, value: value, column: column + position})
		position = closeAt + 1
	}
	return tokens, nil
}

func enhancedWords(p *formatutil.Parser, tokens []token, firstText int, firstStart time.Duration, sourceLine, column int) ([]lyric.Word, time.Duration, error) {
	var words []lyric.Word
	var current strings.Builder
	start := firstStart
	var explicitEnd time.Duration = -1
	for i := firstText; i < len(tokens); i++ {
		if i&255 == 0 {
			if err := p.Check(); err != nil {
				return nil, -1, err
			}
		}
		part := tokens[i]
		if !part.timed {
			current.WriteString(part.text)
			continue
		}
		if current.Len() != 0 {
			words = append(words, lyric.Word{Text: current.String(), Start: start, End: part.value})
			if len(words) > p.Options.Limits.MaxWords {
				return nil, -1, formatutil.Error(sourceLine, column, "limit", "enhanced words exceed word limit")
			}
			current.Reset()
		}
		start = part.value
		if i == len(tokens)-1 {
			explicitEnd = part.value
		}
	}
	if current.Len() != 0 {
		words = append(words, lyric.Word{Text: current.String(), Start: start, End: -1})
	}
	// When a final marker follows the final text segment, the preceding timed
	// token is its start and this terminal marker closes that word. A row with
	// only a terminal marker is still a plain text line with an explicit end.
	if explicitEnd >= 0 && len(words) == 0 {
		return nil, explicitEnd, nil
	}
	if explicitEnd >= 0 && len(words) > 0 && words[len(words)-1].End < 0 {
		words[len(words)-1].End = explicitEnd
	}
	return words, explicitEnd, nil
}

func parseMetadataLine(line string) (key, value string, ok bool) {
	if len(line) < 3 || line[0] != '[' || line[len(line)-1] != ']' {
		return "", "", false
	}
	content := strings.TrimSpace(line[1 : len(line)-1])
	key, value, ok = strings.Cut(content, ":")
	if !ok || !metadataName(strings.TrimSpace(key)) {
		return "", "", false
	}
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	return key, value, true
}

func metadataCandidate(content string) bool {
	key, _, ok := strings.Cut(content, ":")
	if !ok {
		return false
	}
	return metadataName(strings.TrimSpace(key))
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

type translationState struct {
	parser *formatutil.Parser
	bytes  int // Expanded Translation fields retained across all output lines.
}

func (state *translationState) attach(text string, starts []time.Duration, lastMain []int, mainByStart map[time.Duration]int, lines []formatutil.Line, sourceLine, column int) (bool, error) {
	if err := state.parser.Check(); err != nil {
		return false, err
	}
	if strings.TrimSpace(text) == "" {
		return false, nil
	}
	var targets []int
	if len(starts) == 0 {
		targets = lastMain
	} else {
		for targetIndex, start := range uniqueTimes(starts) {
			if targetIndex&255 == 0 {
				if err := state.parser.Check(); err != nil {
					return false, err
				}
			}
			index, ok := mainByStart[start]
			if !ok {
				return false, nil
			}
			targets = append(targets, index)
		}
	}
	if len(targets) == 0 {
		return false, nil
	}
	// Preflight every expanded field before concatenating any strings. Finish
	// later accounts for the rest of the document's text, but cannot protect
	// allocations that translation appends have already made along the way.
	prospective := state.bytes
	limit := state.parser.Options.Limits.MaxTextBytes
	for targetIndex, index := range targets {
		if targetIndex&255 == 0 {
			if err := state.parser.Check(); err != nil {
				return false, err
			}
		}
		if lines[index].Translation != "" {
			if prospective >= limit {
				return false, formatutil.Error(sourceLine, column, "limit", "expanded text byte limit exceeded")
			}
			prospective++ // The separator retained by this target.
		}
		if len(text) > limit-prospective {
			return false, formatutil.Error(sourceLine, column, "limit", "expanded text byte limit exceeded")
		}
		prospective += len(text)
	}
	for targetIndex, index := range targets {
		if targetIndex&255 == 0 {
			if err := state.parser.Check(); err != nil {
				return false, err
			}
		}
		if lines[index].Translation == "" {
			lines[index].Translation = text
		} else {
			lines[index].Translation += "\n" + text
		}
	}
	state.bytes = prospective
	return true, nil
}

func leadingTimeTokens(tokens []token, firstText int) []token {
	if firstText <= 0 {
		return nil
	}
	var result []token
	for i := 0; i < firstText && i < len(tokens); i++ {
		if tokens[i].timed {
			result = append(result, tokens[i])
		}
	}
	return result
}

func tokenTimes(tokens []token) []time.Duration {
	result := make([]time.Duration, len(tokens))
	for i, part := range tokens {
		result[i] = part.value
	}
	return result
}

func leadingTagColumn(tokens []token, value time.Duration, fallback int) int {
	for _, part := range tokens {
		if part.value == value {
			return part.column
		}
	}
	return fallback
}

func uniqueTimes(values []time.Duration) []time.Duration {
	result := make([]time.Duration, 0, len(values))
	seen := make(map[time.Duration]struct{}, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func firstTimedToken(tokens []token) *token {
	for _, part := range tokens {
		if part.timed {
			return &part
		}
	}
	return nil
}

func tokenBeforeText(tokens []token, firstText int) *token {
	if firstText <= 0 || firstText > len(tokens) {
		return nil
	}
	part := &tokens[firstText-1]
	if !part.timed {
		return nil
	}
	return part
}

func hasTimedTokenAfter(tokens []token, index int) bool {
	for i := index + 1; i < len(tokens); i++ {
		if tokens[i].timed {
			return true
		}
	}
	return false
}

func lastTimedToken(tokens []token) *token {
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].timed {
			return &tokens[i]
		}
	}
	return nil
}

func isBackground(text string) bool {
	return len([]rune(text)) > 2 && (strings.HasPrefix(text, "(") || strings.HasPrefix(text, "（")) && (strings.HasSuffix(text, ")") || strings.HasSuffix(text, "）"))
}

func trimBackgroundText(text string) string {
	if strings.HasPrefix(text, "（") {
		text = strings.TrimPrefix(text, "（")
	} else {
		text = strings.TrimPrefix(text, "(")
	}
	if strings.HasSuffix(text, "）") {
		text = strings.TrimSuffix(text, "）")
	} else {
		text = strings.TrimSuffix(text, ")")
	}
	return text
}

func trimBackgroundWords(words []lyric.Word) {
	if len(words) == 0 {
		return
	}
	words[0].Text = trimLeadingParen(words[0].Text)
	last := len(words) - 1
	words[last].Text = trimTrailingParen(words[last].Text)
}

func trimLeadingParen(text string) string {
	if strings.HasPrefix(text, "（") {
		return strings.TrimPrefix(text, "（")
	}
	return strings.TrimPrefix(text, "(")
}

func trimTrailingParen(text string) string {
	if strings.HasSuffix(text, "）") {
		return strings.TrimSuffix(text, "）")
	}
	return strings.TrimSuffix(text, ")")
}

func joinWords(words []lyric.Word) string {
	var text strings.Builder
	for _, word := range words {
		text.WriteString(word.Text)
	}
	return text.String()
}

func timestamp(value string) (time.Duration, error) {
	minutes, rest, ok := strings.Cut(value, ":")
	if !ok || len(minutes) == 0 || len(minutes) > 3 || len(rest) == 0 {
		return 0, fmt.Errorf("expected minutes:ss with optional millisecond fraction")
	}
	for _, c := range minutes {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("timestamp minutes must be nonnegative digits")
		}
	}
	separator := strings.IndexAny(rest, ".:")
	secondsPart := rest
	fractionPart := ""
	if separator >= 0 {
		secondsPart, fractionPart = rest[:separator], rest[separator+1:]
		if fractionPart == "" || len(fractionPart) > 6 {
			return 0, fmt.Errorf("timestamp fraction requires one to six decimal digits")
		}
		for _, c := range fractionPart {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("timestamp fraction must contain decimal digits")
			}
		}
	}
	if len(secondsPart) == 0 || len(secondsPart) > 2 {
		return 0, fmt.Errorf("timestamp seconds require one or two digits")
	}
	for _, c := range secondsPart {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("timestamp seconds require decimal digits")
		}
	}
	seconds, err := strconv.ParseInt(secondsPart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid timestamp seconds")
	}
	var fraction int64
	for i := 0; i < min(3, len(fractionPart)); i++ {
		fraction = fraction*10 + int64(fractionPart[i]-'0')
	}
	for i := len(fractionPart); i < 3; i++ {
		fraction *= 10
	}
	minValue, err := strconv.ParseInt(minutes, 10, 64)
	remaining := seconds*int64(time.Second) + fraction*int64(time.Millisecond)
	if err != nil || minValue > (math.MaxInt64-remaining)/int64(time.Minute) {
		return 0, fmt.Errorf("timestamp exceeds time.Duration range")
	}
	return time.Duration(minValue*int64(time.Minute) + remaining), nil
}

package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// demoDocument contains only synthetic text, including deliberate cluster and
// direction boundaries. Words concatenate exactly to Text; it is not a parser.
func demoDocument(count int) (*lyric.Document, error) {
	if count < 0 || count > 10000 {
		return nil, fmt.Errorf("native example: line count must be between 0 and 10000")
	}
	segments := [][]string{
		{"A ", "new ", "morning ", "on the screen"},
		{"这是", "合成的", "逐词", "文字示例"},
		{"静かな ", "画面で ", "文字を ", "たしかめる"},
		{"مرحبا ", "بالعالم ", "Native UI"},
		{"Emoji ", "👩‍💻 ", "and ", "e", "\u0301"},
		{"An of", "fice ", "with ", "a ligature"},
		{"A longer synthetic line ", "wraps as the window narrows; ", "the whole paragraph keeps its shaping ", "while timed words cross visual lines."},
		{"English ", "עברית ", "and العربية ", "in one line"},
	}
	translations := []string{
		"屏幕上，新的清晨", "A synthetic timed lyric example", "在安静的画面中确认文字",
		"Hello, world — synthetic bilingual text", "Emoji and a combining accent", "连字跨过时间词的边界",
		"缩小窗口，检查整行换行和多个高亮矩形", "三种文字保留各自的阅读方向",
	}
	romanizations := []string{"", "Zhè shì hé chéng de zhú cí wén zì shì lì", "Shizuka na gamen de moji o tashikameru", "", "", "", "", ""}
	languages := []string{"en", "zh-Hans", "ja", "ar", "en", "en", "en", ""}
	lines := make([]lyric.Line, count)
	var start time.Duration
	for i := range lines {
		if i > 0 && i%9 != 1 { // simultaneous starts exercise overlapping vocals
			start += 4 * time.Second
		}
		if i > 0 && i%12 == 0 {
			start += 8 * time.Second // an idle gap must use a timed wakeup
		}
		parts := segments[i%len(segments)]
		words := make([]lyric.Word, len(parts))
		var text strings.Builder
		for j, part := range parts {
			text.WriteString(part)
			words[j] = lyric.Word{Text: part, Start: start + 3*time.Second*time.Duration(j)/time.Duration(len(parts)), End: start + 3*time.Second*time.Duration(j+1)/time.Duration(len(parts))}
		}
		lines[i] = lyric.Line{
			Text: text.String(), Start: start, End: start + 3*time.Second, Words: words,
			Translation: translations[i%len(segments)], Romanization: romanizations[i%len(segments)],
			Language: languages[i%len(segments)], Duet: i%9 == 1, Background: i%16 == 9,
		}
	}
	return lyric.NewDocument(lines)
}

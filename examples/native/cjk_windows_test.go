//go:build windows

package main

import (
	"reflect"
	"testing"
	"unicode/utf16"
)

func TestWindowsICUWordDictionary(t *testing.T) {
	// Synthetic oracle from Intl.Segmenter('en-US',{granularity:'word'}),
	// Chromium 157 / Node ICU 77.1. This records the tested dictionary inputs;
	// it is not a claim that every OS ICU version matches Chromium globally.
	for _, test := range []struct {
		text  string
		words []string
	}{
		{"你好世界", []string{"你好", "世界"}},
		{"日本語の歌詞", []string{"日本語", "の", "歌詞"}},
		{"Hello 世界!", []string{"Hello", "世界"}},
	} {
		segments, ok := platformWordSegments(test.text)
		if !ok {
			t.Skip("Windows public ICU is unavailable")
		}
		units := utf16.Encode([]rune(test.text))
		var got []string
		for _, segment := range segments {
			if segment.wordLike {
				got = append(got, string(utf16.Decode(units[segment.start:segment.end])))
			}
		}
		if !reflect.DeepEqual(got, test.words) {
			t.Fatalf("ICU %q=%q, reference=%q", test.text, got, test.words)
		}
	}
}

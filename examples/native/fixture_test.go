package main

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDemoDocumentSizesAndLimits(t *testing.T) {
	for _, count := range []int{0, 1, 200, 2000, 10000} {
		document, err := demoDocument(count)
		if err != nil {
			t.Fatalf("demoDocument(%d): %v", count, err)
		}
		if document.Len() != count {
			t.Fatalf("demoDocument(%d) has %d lines", count, document.Len())
		}
		for i := 0; i < document.Len(); i++ {
			line := document.Line(i)
			var text strings.Builder
			for j := 0; j < line.WordCount(); j++ {
				text.WriteString(line.Word(j).Text)
			}
			if !utf8.ValidString(line.Text()) || text.String() != line.Text() {
				t.Fatalf("line %d is not valid UTF-8 covered exactly by its words", i)
			}
		}
	}
	for _, count := range []int{-1, 10001} {
		if document, err := demoDocument(count); err == nil || document != nil {
			t.Fatalf("demoDocument(%d) = %v, %v; want nil document and error", count, document, err)
		}
	}
}

func TestDemoDocumentExercisesNativeBoundaries(t *testing.T) {
	document, err := demoDocument(32)
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	var maxEnd time.Duration
	var simultaneous, overlapping, longGap, duet, splitCombining, splitLigature bool
	for i := 0; i < document.Len(); i++ {
		line := document.Line(i)
		text.WriteString(line.Text())
		if i > 0 {
			simultaneous = simultaneous || line.Start() == document.Line(i-1).Start()
			overlapping = overlapping || line.Start() < maxEnd
			longGap = longGap || line.Start()-maxEnd >= 7*time.Second
		}
		maxEnd = max(maxEnd, line.End())
		duet = duet || line.Duet()
		for j := 1; j < line.WordCount(); j++ {
			previous, word := line.Word(j-1), line.Word(j)
			splitCombining = splitCombining || previous.Text == "e" && word.Text == "\u0301"
			splitLigature = splitLigature || strings.HasSuffix(previous.Text, "of") && strings.HasPrefix(word.Text, "fice")
		}
	}
	if !simultaneous || !overlapping || !longGap || !duet {
		t.Fatalf("missing timing scenario: simultaneous=%v overlap=%v longGap=%v duet=%v", simultaneous, overlapping, longGap, duet)
	}
	if !splitCombining || !splitLigature {
		t.Fatalf("missing timing/cluster split: combining=%v ligature=%v", splitCombining, splitLigature)
	}
	for _, sample := range []string{"这是", "静かな", "مرحبا", "עברית", "👩‍💻", "e\u0301"} {
		if !strings.Contains(text.String(), sample) {
			t.Errorf("fixture does not exercise %q", sample)
		}
	}
}

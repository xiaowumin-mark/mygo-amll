package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/format"
)

const hostTTMLFixture = `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:ttm="http://www.w3.org/ns/ttml#metadata"><body><div><p xml:id="host-line" begin="0s" end="2s" ttm:agent="v2"><span begin="0s" end="1s">Alpha </span><span begin="1s" end="2s">beta</span><span ttm:role="x-translation">Translated text</span></p></div></body></tt>`

func writeHostTTML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "synthetic.ttml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadTTMLHostFile(t *testing.T) {
	path := writeHostTTML(t, hostTTMLFixture)
	result, err := loadTTML(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Document == nil || result.Document.Len() != 1 || len(result.Diagnostics) != 0 {
		t.Fatalf("unexpected host parse result: document=%v, diagnostics=%+v", result.Document, result.Diagnostics)
	}
	line := result.Document.Line(0)
	if line.ID() != "host-line" || line.Text() != "Alpha beta" || line.WordCount() != 2 ||
		line.Translation() != "Translated text" || !line.Duet() || line.End() != 2*time.Second {
		t.Fatal("host loader changed timing, text or annotations")
	}
	// A returned immutable document does not depend on the source file remaining.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if line.Word(1).Text != "beta" {
		t.Fatal("loaded document lost its owned text after source removal")
	}
}

func TestLoadTTMLMissingAndParseErrorsIncludePath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.ttml")
	result, err := loadTTML(context.Background(), missing)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), strconv.Quote(missing)) || result.Document != nil {
		t.Fatalf("missing file did not preserve its path and error: %v", err)
	}
	path := writeHostTTML(t, `<tt><body><p begin="0s" end="1s">`)
	result, err = loadTTML(context.Background(), path)
	var parseError *format.ParseError
	if !errors.As(err, &parseError) || !strings.Contains(err.Error(), strconv.Quote(path)) || result.Document != nil {
		t.Fatalf("parse failure did not preserve its path and typed diagnostic: %v", err)
	}
}

func TestLoadTTMLByteBudget(t *testing.T) {
	limit := format.DefaultLimits().MaxBytes
	// Source whitespace is outside the paragraph, so expanded output stays small.
	path := writeHostTTML(t, hostTTMLFixture+strings.Repeat(" ", limit-len(hostTTMLFixture)))
	if _, err := loadTTML(context.Background(), path); err != nil {
		t.Fatalf("exactly the default byte budget should be accepted: %v", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, writeError := file.WriteString(" ")
	closeError := file.Close()
	if writeError != nil || closeError != nil {
		t.Fatalf("extend fixture: write=%v close=%v", writeError, closeError)
	}
	result, err := loadTTML(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "input byte limit exceeded") ||
		!strings.Contains(err.Error(), strconv.Quote(path)) || result.Document != nil {
		t.Fatalf("8 MiB + 1 byte did not fail the host read limit: %v", err)
	}
}

func TestLoadTTMLNativeDocumentLineBudget(t *testing.T) {
	// One physical line isolates the expanded line limit from the source limit.
	path := writeHostTTML(t, `<tt><body>`+strings.Repeat(`<p begin="0s" end="1s">Synthetic</p>`, 10001)+`</body></tt>`)
	result, err := loadTTML(context.Background(), path)
	var parseError *format.ParseError
	if !errors.As(err, &parseError) || parseError.Code != "limit" || result.Document != nil {
		t.Fatalf("native example accepted more than 10,000 output lines: %v", err)
	}
	path = writeHostTTML(t, hostTTMLFixture+strings.Repeat("\n", 10000)+" ")
	result, err = loadTTML(context.Background(), path)
	if !errors.As(err, &parseError) || parseError.Code != "limit" || result.Document != nil {
		t.Fatalf("native example accepted more than 10,000 physical lines: %v", err)
	}
}

func TestLoadTTMLCancellation(t *testing.T) {
	path := writeHostTTML(t, hostTTMLFixture)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := loadTTML(ctx, path)
	if !errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), strconv.Quote(path)) || result.Document != nil {
		t.Fatalf("pre-cancellation did not preserve context error and path: %v", err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Unix(0, 0))
	defer cancel()
	if _, err := loadTTML(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired deadline was not preserved: %v", err)
	}
	if _, err := loadTTML(nil, path); err == nil || !strings.Contains(err.Error(), strconv.Quote(path)) {
		t.Fatalf("nil context was not rejected with its file path: %v", err)
	}
}

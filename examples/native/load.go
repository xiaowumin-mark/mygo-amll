package main

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/format/ttml"
)

// loadTTML is example-host I/O, kept outside the in-memory format packages. It
// reads at most the default byte budget plus one sentinel byte and limits both
// physical and output lines to the native example's 10,000-line document budget.
// Cancellation is checked before/after the bounded read and within the parser;
// it cannot interrupt an operating-system file read already in progress.
func loadTTML(ctx context.Context, path string) (format.Result, error) {
	if ctx == nil {
		return format.Result{}, fmt.Errorf("native: load TTML %q: nil context", path)
	}
	if err := ctx.Err(); err != nil {
		return format.Result{}, fmt.Errorf("native: load TTML %q: %w", path, err)
	}
	file, err := os.Open(path)
	if err != nil {
		return format.Result{}, fmt.Errorf("native: load TTML %q: %w", path, err)
	}
	defer file.Close()
	limit := format.DefaultLimits().MaxBytes
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return format.Result{}, fmt.Errorf("native: load TTML %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return format.Result{}, fmt.Errorf("native: load TTML %q: %w", path, err)
	}
	if len(data) > limit {
		return format.Result{}, fmt.Errorf("native: load TTML %q: input byte limit exceeded (%d bytes)", path, limit)
	}
	result, err := ttml.Parse(ctx, data, format.Options{Limits: format.Limits{MaxLines: 10000}})
	if err != nil {
		return result, fmt.Errorf("native: load TTML %q: %w", path, err)
	}
	return result, nil
}

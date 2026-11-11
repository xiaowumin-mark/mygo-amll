package corpuscheck

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

const synthetic = `<tt xmlns="http://www.w3.org/ns/ttml" xmlns:i="http://music.apple.com/lyric-ttml-internal" xmlns:m="http://www.w3.org/ns/ttml#metadata"><head><metadata><meta key="musicName" value="Synthetic"/><meta key="artists" value="Example"/><meta key="artists" value="Example"/></metadata></head><body><p begin="1s" end="3s" i:key="L1"><span begin="1s" end="2s">A</span> <span begin="2s" end="3s">B</span><span m:role="x-translation">合成</span></p></body></tt>`

func TestRunDiscoversTrackedPathsAndRetainsDeterminism(t *testing.T) {
	root := repository(t, map[string]string{
		"raw-lyrics/a.ttml":  synthetic,
		"Issue-files/b.TTML": synthetic,
		"am-lyrics/c.ttml":   synthetic,
		"ignored.txt":        "not a lyric",
	})
	if err := os.WriteFile(filepath.Join(root, "untracked.ttml"), []byte("malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	var serial, parallel bytes.Buffer
	first, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root, Workers: 1, Records: &serial, Canonical: true})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root, Workers: 3, Records: &parallel, Canonical: true})
	if err != nil {
		t.Fatal(err)
	}
	if first.Files != 3 || second.Files != 3 || first.UniqueInputs != 1 || len(first.Failures) != 0 {
		t.Fatalf("incomplete or failed run: %+v", first)
	}
	if !bytes.Equal(serial.Bytes(), parallel.Bytes()) {
		t.Fatal("record order or contents depend on worker scheduling")
	}
	decoder := json.NewDecoder(bytes.NewReader(serial.Bytes()))
	paths := []string{}
	for decoder.More() {
		var record Record
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, record.Path)
		if record.SourceSHA256 == "" || record.SnapshotSHA256 == "" || record.LineCount != 1 || record.WordCount != 2 || record.Error != "" {
			t.Fatalf("incomplete evidence: %+v", record)
		}
		if record.Canonical == nil || record.Canonical.Lines[0].Words[0].Text != "A " || record.Canonical.Lines[0].Translation != "合成" {
			t.Fatalf("canonical text changed: %+v", record.Canonical)
		}
	}
	if strings.Join(paths, ",") != "Issue-files/b.TTML,am-lyrics/c.ttml,raw-lyrics/a.ttml" {
		t.Fatalf("paths were skipped or reordered: %v", paths)
	}
}

func TestRunReportsParseErrorsWithoutStoppingOtherFiles(t *testing.T) {
	root := repository(t, map[string]string{"a.ttml": "<tt>", "b.ttml": synthetic})
	var records bytes.Buffer
	report, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root, Records: &records})
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 2 || len(report.Failures) != 1 || report.Failures[0].Path != "a.ttml" || report.Lines != 1 {
		t.Fatalf("run skipped failures or remaining files: %+v", report)
	}
	if bytes.Contains(records.Bytes(), []byte(`"canonical":`)) || bytes.Contains(records.Bytes(), []byte("合成")) {
		t.Fatal("default report leaked lyric contents")
	}
}

func TestPinnedCorpusAndCancellation(t *testing.T) {
	root := repository(t, map[string]string{"a.ttml": synthetic})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Config{Corpus: root, ParserRoot: root}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled run returned %v", err)
	}
	if err := CheckCancellation(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root, ExpectedCorpusCommit: strings.Repeat("0", 40)}); err == nil {
		t.Fatal("mismatched corpus revision passed")
	}
	if err := os.WriteFile(filepath.Join(root, "a.ttml"), []byte(synthetic+" "), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root}); err == nil || !strings.Contains(err.Error(), "tracked changes") {
		t.Fatalf("dirty corpus passed: %v", err)
	}
}

func TestDefaultBudgetAndTrackedBlobAreEnforced(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.ttml")
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), common.DefaultLimits().MaxBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	result := checkFile(context.Background(), root, trackedFile{path: "large.ttml", mode: "100644"}, false)
	if !strings.Contains(result.Error, "default input budget") {
		t.Fatalf("default budget was relaxed: %+v", result)
	}
	if err := os.WriteFile(path, []byte(synthetic), 0o600); err != nil {
		t.Fatal(err)
	}
	result = checkFile(context.Background(), root, trackedFile{path: "large.ttml", mode: "100644", blob: strings.Repeat("0", 40)}, false)
	if !strings.Contains(result.Error, "Git blob") {
		t.Fatalf("untracked content passed: %+v", result)
	}
	result = checkFile(context.Background(), root, trackedFile{path: "large.ttml", mode: "120000"}, false)
	if !strings.Contains(result.Error, "not a regular") {
		t.Fatalf("symlink mode passed: %+v", result)
	}
}

func TestSourceChecksDoNotChangeAuthoredWordOrderOrPointTiming(t *testing.T) {
	input := `<tt xmlns:i="http://music.apple.com/lyric-ttml-internal"><body><p i:key="L1" begin="1s" end="4s"><span begin="3s" end="4s">A</span><span begin="1s" end="2s">B</span><span begin="2s" end="2s">C</span></p></body></tt>`
	root := repository(t, map[string]string{"a.ttml": input})
	var records bytes.Buffer
	report, err := Run(context.Background(), Config{Corpus: root, ParserRoot: root, Records: &records, Canonical: true})
	if err != nil || len(report.Failures) != 0 {
		t.Fatalf("authored point/nonmonotonic words rejected: %+v %v", report, err)
	}
	var record Record
	if err := json.Unmarshal(bytes.TrimSpace(records.Bytes()), &record); err != nil {
		t.Fatal(err)
	}
	words := record.Canonical.Lines[0].Words
	if len(words) != 3 || words[0].Text != "A" || words[1].Text != "B" || words[2].Text != "C" || words[2].StartMS != words[2].EndMS {
		t.Fatalf("authored content silently changed: %+v", words)
	}
}

func repository(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git is required for corpus tooling tests")
	}
	root := t.TempDir()
	for path, content := range files {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "--quiet"},
		{"-c", "core.autocrlf=false", "add", "--all"},
		{"-c", "user.name=Corpus Test", "-c", "user.email=corpus@example.invalid", "-c", "core.hooksPath=" + filepath.Join(root, "empty-hooks"), "commit", "--no-gpg-sign", "--quiet", "-m", "synthetic corpus"},
	} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git fixture %v: %s: %v", args, output, err)
		}
	}
	return root
}

func TestCheckoutCRLFIsProvenWithoutChangingParserInput(t *testing.T) {
	root := t.TempDir()
	input := []byte(synthetic + "\r\n")
	path := filepath.Join(root, "a.ttml")
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	blob := blobHash([]byte(synthetic+"\n"), 40)
	record := checkFile(context.Background(), root, trackedFile{path: "a.ttml", mode: "100644", blob: blob}, true)
	if record.Error != "" || !record.CheckoutCRLF || record.SourceSHA256 != digest(input) || record.Bytes != len(input) {
		t.Fatalf("checkout transform was not identified exactly: %+v", record)
	}
	if record.Canonical == nil || record.Canonical.Lines[0].Words[0].Text != "A " {
		t.Fatal("checkout transform changed parsed contents")
	}
}

func TestReversedAndOutsideEndpointsAreReportedWithoutRepair(t *testing.T) {
	doc, err := lyric.NewDocumentWithOptions([]lyric.Line{{Start: time.Second, End: 4 * time.Second, Words: []lyric.Word{
		{Text: "A", Start: 3 * time.Second, End: 2 * time.Second, Ruby: []lyric.RubyWord{{Text: "a", Start: 5 * time.Second, End: time.Second}}},
		{Text: "B", Start: 0, End: 0},
	}}}, nil, lyric.DocumentOptions{PreserveSourceTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := invariants(doc); err != nil {
		t.Fatalf("authored timing was rejected: %v", err)
	}
	stats := timingStats(doc)
	if stats.ReversedWords != 1 || stats.ReversedRuby != 1 || stats.OutsideWords != 1 || stats.OutsideRuby != 1 || stats.PointWords != 1 || stats.NonmonotonicWords != 1 {
		t.Fatalf("timing anomalies were not reported: %+v", stats)
	}
	canonical := canonicalize(doc)
	if canonical.Lines[0].Words[0].StartMS != 3000 || canonical.Lines[0].Words[0].EndMS != 2000 || canonical.Lines[0].Words[0].Ruby[0].StartMS != 5000 || canonical.Lines[0].Words[0].Ruby[0].EndMS != 1000 {
		t.Fatalf("canonical output repaired source timing: %+v", canonical)
	}
}

func TestSourceReversedLineIsReportedWithoutRepair(t *testing.T) {
	doc, err := lyric.NewDocumentWithOptions([]lyric.Line{{Start: 2 * time.Second, End: time.Second, Text: "Synthetic"}}, nil, lyric.DocumentOptions{PreserveSourceTiming: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := invariants(doc); err != nil || timingStats(doc).ReversedLines != 1 {
		t.Fatalf("source line rejected or not reported: %v %+v", err, timingStats(doc))
	}
	if canonicalize(doc).Lines[0].EndMS != 1000 {
		t.Fatal("source line endpoint changed")
	}
}

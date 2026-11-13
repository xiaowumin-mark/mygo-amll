//go:build windows

// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// These are independently captured DOM/CDP facts for synthetic strings, not
// Native-produced goldens or a claim about arbitrary installed/custom fonts.
// Recorded 2026-11-10: Windows 11 10.0.26300, Chrome 157.0.8103.38 revision
// a733b5afbb3ba618844ff8876b89ad1a774b2f18, en element language, ltr paragraph,
// 48 CSS px, DPR 1. The host explicitly sets "Segoe UI",sans-serif, as does the
// reference harness. Empty Family inherits that host; it is NOT a browser's UA
// default family. Its captured computed family is retained separately below.
// Segoe UI Emoji is version 1.70; Segoe UI Symbol is 6.24. The font fingerprints
// scope this recording to the actual system faces, including Hebrew fallbacks.
// Both weights select SegoeUIEmoji for the four color examples and the emoji
// in the RTL context. VS15 selects SegoeUISymbol. Italic has the same selected
// face and advance but visibly shears emoji in the independent browser grid;
// this recording must not be used to authorize Native italic substitution.
const referenceReplacementChrome = "Chrome/157.0.8103.38"

type referenceReplacementFont struct {
	FamilyName     string `json:"familyName"`
	PostScriptName string `json:"postScriptName"`
	GlyphCount     int    `json:"glyphCount"`
	IsCustomFont   bool   `json:"isCustomFont"`
}

type referenceReplacementMeasure struct {
	Width, Height               float64
	Font, Family, Weight, Style string
	Direction                   string
}

type referenceReplacementVector struct {
	Family, Text, Sample string
	Weight               int
	Italic               bool
	Fonts                []referenceReplacementFont
	Measure              referenceReplacementMeasure
}

type referenceReplacementVariant struct {
	Family, Font, ComputedFamily string
	Weight                       int
	Italic                       bool
	RTLWidth                     float64
	RTLFonts                     []referenceReplacementFont
}

var referenceReplacementFontHashes = map[string]string{
	"arial.ttf":    "edfbd22a3d088fc2f5d7c79b8c9c732df6dc878a60f0a80be0f92a3c7a634404",
	"arialbd.ttf":  "4d59199834e7439c67ea4949407639961060b21b173833f566eb60ac6fd8f5af",
	"arialbi.ttf":  "d68bdf8ebfe67a72e6db02b104e8062de5e75c358ba9bb7ef5d0e5c70272a1c5",
	"ariali.ttf":   "4487a06c1b8de98deb0cab2c04a876cbc8f31b7e397dc1f2b2021c0746321d7c",
	"calibri.ttf":  "57e93ff143572bcc092c1f56202d43ddc07bb119aa104c6d7af163470fb551f6",
	"calibrib.ttf": "0a8296747536ead27d5f48f67e911cc3b207742b3f704e66995251d12fd422e5",
	"calibrii.ttf": "7f150e9eabe5db3e1b0f9469dca5c96176bc2fbb306dc9d48b7029dc69c52763",
	"calibriz.ttf": "760539bfedbe24e7853ca8a621d9cb1f3de52036c116e81aae36b33c4ab60a7c",
	"consola.ttf":  "cf00b507b3286870cc5064ebd0633c303f70b491a4af25eec2d32df413db0179",
	"consolab.ttf": "7e8de70c92a891722bd3fbd623db005caab2bfe267e5dab5be46b95a44e0c7f1",
	"consolai.ttf": "0b5b0484cf69ae973bd39380069f141271a4c9299b8885fa2d13f42aadbefef9",
	"consolaz.ttf": "9bc225ec52aa8f4b88b98bb735b1908bd72c9e31a12139fafcc1bd6c86bed3b4",
	"segoeui.ttf":  "704eada12961a27fd271f7d3b09e2066520e758390c1776744eff430fee30f6e",
	"segoeuii.ttf": "f9feaeee8b20751c15700b4fb03571b335f52e91edece7fb7aa1ceaa6eeb0db7",
	"seguiemj.ttf": "451df9bb5546055af625ff86aa72695df43e8e170b4c6c1316fe44e9c7a22f7c",
	"seguisb.ttf":  "6c3fd894b19950b10c34bf64069ef821eaf2f58f959146306fee5b367472e501",
	"seguisbi.ttf": "16847ed882097cd1c0d45c63b6d75c3021f2133c64db03796f57dac198d0b2ad",
	"seguisym.ttf": "a4a35dcc62cd30e1a6c97b695ecef83e59d2b149e3af834f6a49f11851d56b37",
	"tahoma.ttf":   "9af03d4ad44a3b413d92f7de48b94aa7cc8a1471a75d498406eae837f62ee1d1",
	"tahomabd.ttf": "b16ed1a15e0cabea8d41d9f8899c253cd61ac466af5e3980d02bd0cf311c2267",
	"times.ttf":    "0bf1b21b27b370039b60eca23d532c3fe3fb308750d8f13816db70a6cb666804",
	"timesbd.ttf":  "cb8f85dedb0ab01792baad20da28d26773485ac9c21e112bb5c511a8b2001eee",
	"timesbi.ttf":  "5d78b5c66ac8e9c3fc94bd4ead792e1127f5f3faa29f768ff9b599339afe95bf",
	"timesi.ttf":   "11e6aae7436a5f2442eada66493d1767c78dde02acedc0afa6e7d0afeb0f3a14",
}

// Each row is one actual family/weight/style DOM recording. Common sample
// widths and selected faces were identical across all 24 rows, so they are
// factored below; the context-dependent Hebrew fonts and widths remain explicit.
var referenceReplacementVariants = []referenceReplacementVariant{
	{"Segoe UI", "48px / 57.6px \"Segoe UI\"", "\"Segoe UI\"", 400, false, 129.78125, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI", "SegoeUI", 4, false}}},
	{"Segoe UI", "italic 48px / 57.6px \"Segoe UI\"", "\"Segoe UI\"", 400, true, 131.484375, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI", "SegoeUI", 2, false}, {"Segoe UI", "SegoeUI-Italic", 2, false}}},
	{"Segoe UI", "600 48px / 57.6px \"Segoe UI\"", "\"Segoe UI\"", 600, false, 133.25, []referenceReplacementFont{{"Segoe UI Semibold", "SegoeUI-Semibold", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"Segoe UI", "italic 600 48px / 57.6px \"Segoe UI\"", "\"Segoe UI\"", 600, true, 132.140625, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI Semibold", "SegoeUI-SemiboldItalic", 2, false}, {"Segoe UI", "SegoeUI", 2, false}}},
	{"Arial", "48px / 57.6px Arial", "Arial", 400, false, 124.765625, []referenceReplacementFont{{"Arial", "ArialMT", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"Arial", "italic 48px / 57.6px Arial", "Arial", 400, true, 124.765625, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Arial", "Arial-ItalicMT", 4, false}}},
	{"Arial", "600 48px / 57.6px Arial", "Arial", 600, false, 129.96875, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Arial", "Arial-BoldMT", 4, false}}},
	{"Arial", "italic 600 48px / 57.6px Arial", "Arial", 600, true, 129.96875, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Arial", "Arial-BoldItalicMT", 4, false}}},
	{"Times New Roman", "48px / 57.6px \"Times New Roman\"", "\"Times New Roman\"", 400, false, 117.984375, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Times New Roman", "TimesNewRomanPSMT", 4, false}}},
	{"Times New Roman", "italic 48px / 57.6px \"Times New Roman\"", "\"Times New Roman\"", 400, true, 116.953125, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Times New Roman", "TimesNewRomanPS-ItalicMT", 4, false}}},
	{"Times New Roman", "600 48px / 57.6px \"Times New Roman\"", "\"Times New Roman\"", 600, false, 120.359375, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Times New Roman", "TimesNewRomanPS-BoldMT", 4, false}}},
	{"Times New Roman", "italic 600 48px / 57.6px \"Times New Roman\"", "\"Times New Roman\"", 600, true, 123.015625, []referenceReplacementFont{{"Times New Roman", "TimesNewRomanPS-BoldItalicMT", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"Calibri", "48px / 57.6px Calibri", "Calibri", 400, false, 120.984375, []referenceReplacementFont{{"Calibri", "Calibri", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"Calibri", "italic 48px / 57.6px Calibri", "Calibri", 400, true, 120.75, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Calibri", "Calibri-Italic", 4, false}}},
	{"Calibri", "600 48px / 57.6px Calibri", "Calibri", 600, false, 121.78125, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Calibri", "Calibri-Bold", 4, false}}},
	{"Calibri", "italic 600 48px / 57.6px Calibri", "Calibri", 600, true, 121.453125, []referenceReplacementFont{{"Calibri", "Calibri-BoldItalic", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"Consolas", "48px / 57.6px Consolas", "Consolas", 400, false, 155.765625, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Consolas", "Consolas", 2, false}, {"Segoe UI", "SegoeUI", 2, false}}},
	{"Consolas", "italic 48px / 57.6px Consolas", "Consolas", 400, true, 160.828125, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Tahoma", "Tahoma", 2, false}, {"Consolas", "Consolas-Italic", 2, false}}},
	{"Consolas", "600 48px / 57.6px Consolas", "Consolas", 600, false, 158.25, []referenceReplacementFont{{"Segoe UI Semibold", "SegoeUI-Semibold", 2, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Consolas", "Consolas-Bold", 2, false}}},
	{"Consolas", "italic 600 48px / 57.6px Consolas", "Consolas", 600, true, 168, []referenceReplacementFont{{"Tahoma", "Tahoma-Bold", 2, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Consolas", "Consolas-BoldItalic", 2, false}}},
	{"", "48px / 57.6px \"Segoe UI\", sans-serif", "\"Segoe UI\", sans-serif", 400, false, 129.78125, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI", "SegoeUI", 4, false}}},
	{"", "italic 48px / 57.6px \"Segoe UI\", sans-serif", "\"Segoe UI\", sans-serif", 400, true, 131.484375, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI", "SegoeUI", 2, false}, {"Segoe UI", "SegoeUI-Italic", 2, false}}},
	{"", "600 48px / 57.6px \"Segoe UI\", sans-serif", "\"Segoe UI\", sans-serif", 600, false, 133.25, []referenceReplacementFont{{"Segoe UI Semibold", "SegoeUI-Semibold", 4, false}, {"Segoe UI Emoji", "SegoeUIEmoji", 4, false}}},
	{"", "italic 600 48px / 57.6px \"Segoe UI\", sans-serif", "\"Segoe UI\", sans-serif", 600, true, 132.140625, []referenceReplacementFont{{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}, {"Segoe UI Semibold", "SegoeUI-SemiboldItalic", 2, false}, {"Segoe UI", "SegoeUI", 2, false}}},
}

func referenceReplacementVectors() []referenceReplacementVector {
	samples := []struct {
		name, text string
		width      float64
		font       referenceReplacementFont
	}{
		{"default", "😀", 65.90625, referenceReplacementFont{"Segoe UI Emoji", "SegoeUIEmoji", 1, false}},
		{"vs16", "❤️", 65.90625, referenceReplacementFont{"Segoe UI Emoji", "SegoeUIEmoji", 1, false}},
		{"keycap", "1️⃣", 65.90625, referenceReplacementFont{"Segoe UI Emoji", "SegoeUIEmoji", 1, false}},
		{"family", "👨‍👩‍👧‍👦", 60.140625, referenceReplacementFont{"Segoe UI Emoji", "SegoeUIEmoji", 4, false}},
		{"rtl", "אב 👨‍👩‍👧‍👦!", 0, referenceReplacementFont{}},
		{"vs15", "❤︎", 46.640625, referenceReplacementFont{"Segoe UI Symbol", "SegoeUISymbol", 1, false}},
	}
	out := make([]referenceReplacementVector, 0, len(referenceReplacementVariants)*len(samples))
	for _, variant := range referenceReplacementVariants {
		style := "normal"
		if variant.Italic {
			style = "italic"
		}
		for _, sample := range samples {
			width, fonts := sample.width, []referenceReplacementFont{sample.font}
			if sample.name == "rtl" {
				width, fonts = variant.RTLWidth, slices.Clone(variant.RTLFonts)
			}
			out = append(out, referenceReplacementVector{
				Family: variant.Family, Weight: variant.Weight, Italic: variant.Italic,
				Text: sample.text, Sample: sample.name, Fonts: fonts,
				Measure: referenceReplacementMeasure{Width: width, Height: 57.59375,
					Font: variant.Font, Family: variant.ComputedFamily, Weight: strconv.Itoa(variant.Weight), Style: style, Direction: "ltr"},
			})
		}
	}
	return out
}

func requireReferenceReplacementFonts(t *testing.T) {
	t.Helper()
	for name, want := range referenceReplacementFontHashes {
		data, err := os.ReadFile(filepath.Join(os.Getenv("WINDIR"), "Fonts", name))
		if err != nil {
			t.Skipf("recorded Windows font environment unavailable: %s: %v", name, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != want {
			t.Skipf("%s differs from the recorded Chrome 157 font environment; recapture the independent DOM/CDP oracle", name)
		}
	}
}

// This intentionally gates assertions on the provider's declared capability.
// The recording does not itself expand that capability: a newly enabled known
// family automatically enters these independent DOM-width assertions. Skipped
// families remain unsupported, not validated. This provider does not borrow
// the host's mono resolver, so its VS15 baseline is excluded here. The shared
// production mono policy is instead exercised against independent text/default
// and VS15 facts in fallback_text_emoji_windows_test.go. This is not a Native
// raster test.
func TestReferenceFontReplacementDOMWidths(t *testing.T) {
	requireReferenceReplacementFonts(t)
	provider, _ := testReferenceEmojiProvider(t)
	for _, vector := range referenceReplacementVectors() {
		if vector.Italic || vector.Sample == "vs15" {
			continue
		}
		name := fmt.Sprintf("%s/%d/%s", vector.Family, vector.Weight, vector.Sample)
		t.Run(name, func(t *testing.T) {
			font := ui.Font{Family: vector.Family, Weight: vector.Weight, Size: 48, Lang: "en"}
			if !provider.supportsEmojiFamily(font) {
				t.Skip("provider does not declare this family supported; browser facts alone do not enable it")
			}
			got := provider.Shape(vector.Text, font)
			if got.Err != nil || !got.Exact {
				t.Fatalf("shape: exact=%v err=%v", got.Exact, got.Err)
			}
			if delta := math.Abs(float64(got.Width) - vector.Measure.Width); delta > .06 {
				t.Fatalf("Native width %.8f, independent DOM %.8f, delta %.8f", got.Width, vector.Measure.Width, delta)
			}
			if (len(got.Colors) != 0) != (vector.Sample != "vs15") {
				t.Fatalf("presentation %q: colors=%d", vector.Sample, len(got.Colors))
			}
		})
	}
}

func TestReferenceFontReplacementItalicRemainsUnsupported(t *testing.T) {
	provider, _ := testReferenceEmojiProvider(t)
	for _, variant := range referenceReplacementVariants {
		if variant.Italic && provider.supportsEmojiFamily(ui.Font{Family: variant.Family, Weight: variant.Weight, Italic: true}) {
			t.Errorf("family %q weight %d: selected face/width do not prove the browser's synthetic italic transform", variant.Family, variant.Weight)
		}
	}
}

func referenceReplacementSortedFonts(fonts []referenceReplacementFont) []referenceReplacementFont {
	copy := slices.Clone(fonts)
	slices.SortFunc(copy, func(a, b referenceReplacementFont) int {
		return strings.Compare(a.FamilyName+"/"+a.PostScriptName, b.FamilyName+"/"+b.PostScriptName)
	})
	return copy
}

// Opt-in original browser oracle. Python 3.11+ uses only its standard library
// for CDP; Chrome owns an isolated temporary profile and no visible window.
// CSS.getPlatformFontsForNode independently checks actual platform face names,
// PostScript names and glyph counts; equal advances alone are insufficient.
func TestReferenceFontReplacementAgainstChromium(t *testing.T) {
	chrome := os.Getenv("AMLL_CHROME")
	if chrome == "" {
		t.Skip("set AMLL_CHROME to run the isolated font replacement DOM/CDP oracle")
	}
	requireReferenceReplacementFonts(t)
	temp := t.TempDir()
	script := filepath.Join(temp, "font-replacement.py")
	if err := os.WriteFile(script, []byte(referenceReplacementProbe), 0600); err != nil {
		t.Fatal(err)
	}
	want := referenceReplacementVectors()
	input, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python", script, chrome, temp)
	command.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		t.Fatalf("font replacement CDP oracle: %v\n%s", err, stderr.String())
	}
	var got struct {
		Browser struct{ Product, Revision string }
		DPR     float64
		Rows    []referenceReplacementVector
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("font replacement CDP JSON: %v\n%s", err, output)
	}
	if got.Browser.Product != referenceReplacementChrome {
		t.Skipf("captured %s %s; fixed oracle scope is %s", got.Browser.Product, got.Browser.Revision, referenceReplacementChrome)
	}
	if got.DPR != 1 || len(got.Rows) != len(want) {
		t.Fatalf("oracle DPR/count differs: DPR=%g rows=%d want=%d", got.DPR, len(got.Rows), len(want))
	}
	for i, vector := range want {
		row := got.Rows[i]
		if row.Family != vector.Family || row.Text != vector.Text || row.Weight != vector.Weight || row.Italic != vector.Italic || row.Sample != vector.Sample {
			t.Fatalf("oracle row %d input mapping differs", i)
		}
		if !reflect.DeepEqual(referenceReplacementSortedFonts(row.Fonts), referenceReplacementSortedFonts(vector.Fonts)) {
			t.Errorf("row %d %q/%d/italic=%v/%s actual platform fonts=%+v, recorded=%+v", i, vector.Family, vector.Weight, vector.Italic, vector.Sample, row.Fonts, vector.Fonts)
		}
		if row.Measure != vector.Measure {
			t.Errorf("row %d DOM=%+v, recorded=%+v", i, row.Measure, vector.Measure)
		}
	}
	t.Logf("%s %s: %d independent DOM/platform-font vectors; browser grid captured in isolated test directory", got.Browser.Product, got.Browser.Revision, len(want))
}

const referenceReplacementProbe = `
import base64, html, json, os, platform, socket, struct, subprocess, sys, time, urllib.request
from pathlib import Path
from urllib.parse import urlparse
class CDP:
 def __init__(self,url):
  u=urlparse(url);self.s=socket.create_connection((u.hostname,u.port),timeout=15)
  key=base64.b64encode(os.urandom(16)).decode()
  self.s.sendall((f'GET {u.path} HTTP/1.1\r\nHost: {u.hostname}:{u.port}\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: {key}\r\nSec-WebSocket-Version: 13\r\nOrigin: http://127.0.0.1\r\n\r\n').encode());buf=b''
  while b'\r\n\r\n' not in buf:buf+=self.read(1)
  if b' 101 ' not in buf:raise RuntimeError(buf)
  self.i=0
 def read(self,n):
  chunks=[]
  while n:
   b=self.s.recv(n)
   if not b:raise RuntimeError('CDP disconnected')
   chunks.append(b);n-=len(b)
  return b''.join(chunks)
 def call(self,method,params=None):
  self.i+=1;data=json.dumps({'id':self.i,'method':method,'params':params or {}}).encode();mask=os.urandom(4);n=len(data)
  header=bytes([0x81,0x80|n]) if n<126 else bytes([0x81,0xfe])+struct.pack('!H',n) if n<65536 else bytes([0x81,0xff])+struct.pack('!Q',n)
  self.s.sendall(header+mask+bytes(c^mask[i%4] for i,c in enumerate(data)))
  while True:
   a,b=self.read(2);n=b&127
   if n==126:n=struct.unpack('!H',self.read(2))[0]
   elif n==127:n=struct.unpack('!Q',self.read(8))[0]
   msg=json.loads(self.read(n))
   if msg.get('id')==self.i:
    if 'error' in msg:raise RuntimeError(msg)
    return msg.get('result',{})
root=Path(sys.argv[2]);profile=root/'profile';rows=json.loads(sys.stdin.buffer.read().decode('utf-8'));body=[]
for i,r in enumerate(rows):
 if i%6==0:
  title=f'{r["Family"] or "inherited Segoe UI,sans-serif"} / {r["Weight"]} / {"italic" if r["Italic"] else "normal"}'
  body.append('<div class="row"><div class="label">'+html.escape(title)+'</div>')
 style=f'font-weight:{r["Weight"]};font-style:{"italic" if r["Italic"] else "normal"};'+('font-family:'+json.dumps(r['Family']) if r['Family'] else '')
 body.append(f'<div class="cell"><span id="r{i}" lang="en" dir="ltr" style="{html.escape(style,quote=True)}">{html.escape(r["Text"])}</span></div>')
 if i%6==5:body.append('</div>')
page="""<!doctype html><meta charset="utf-8"><title>Synthetic font replacement oracle</title><style>
*{box-sizing:border-box}body{margin:16px;background:#131722;color:white;font-family:"Segoe UI",sans-serif}h1{font-size:18px}.header,.row{display:grid;grid-template-columns:245px repeat(6,165px);gap:4px}.header{font-size:14px;padding-bottom:8px}.row{height:74px;align-items:center;border-top:1px solid #344052}.label{font-size:12px}.cell{height:72px;display:flex;align-items:center;justify-content:center;background:#192130}span{display:inline-block;font-size:48px;line-height:1.2;white-space:pre}
</style><h1>Chrome actual platform fonts: synthetic replacement matrix (48 CSS px, direction:ltr)</h1><div class="header"><div>primary / weight / style</div>"""+''.join('<div>'+r['Sample']+'</div>' for r in rows[:6])+'</div>'+''.join(body)
(root/'probe.html').write_text(page,encoding='utf-8')
p=subprocess.Popen([sys.argv[1],'--headless=new','--remote-debugging-port=0','--remote-allow-origins=http://127.0.0.1',f'--user-data-dir={profile}','--no-first-run','--no-default-browser-check','--window-size=1290,1900',(root/'probe.html').as_uri()],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
try:
 for _ in range(100):
  if (profile/'DevToolsActivePort').exists():break
  if p.poll() is not None:raise RuntimeError('Chrome exited before debugger was ready')
  time.sleep(.05)
 port=(profile/'DevToolsActivePort').read_text().splitlines()[0]
 pages=json.load(urllib.request.urlopen(f'http://127.0.0.1:{port}/json',timeout=15))
 page=next(v for v in pages if v['type']=='page');c=CDP(page['webSocketDebuggerUrl']);c.call('DOM.enable');c.call('CSS.enable');c.call('Page.enable')
 c.call('Runtime.evaluate',{'expression':'document.fonts.ready.then(()=>true)','awaitPromise':True,'returnByValue':True})
 doc=c.call('DOM.getDocument')['root']['nodeId']
 for i,r in enumerate(rows):
  node=c.call('DOM.querySelector',{'nodeId':doc,'selector':f'#r{i}'})['nodeId'];r['Fonts']=c.call('CSS.getPlatformFontsForNode',{'nodeId':node})['fonts']
  js='JSON.stringify((()=>{let e=document.getElementById('+json.dumps('r'+str(i))+'),s=getComputedStyle(e),r=e.getBoundingClientRect();return {width:r.width,height:r.height,font:s.font,family:s.fontFamily,weight:s.fontWeight,style:s.fontStyle,direction:s.direction}})())'
  r['Measure']=json.loads(c.call('Runtime.evaluate',{'expression':js,'returnByValue':True})['result']['value'])
 metrics=c.call('Page.getLayoutMetrics')['cssContentSize'];shot=c.call('Page.captureScreenshot',{'format':'png','captureBeyondViewport':True,'clip':{'x':0,'y':0,'width':metrics['width'],'height':metrics['height'],'scale':1}});(root/'chrome-grid.png').write_bytes(base64.b64decode(shot['data']))
 result={'Browser':c.call('Browser.getVersion'),'System':platform.platform(),'DPR':c.call('Runtime.evaluate',{'expression':'devicePixelRatio','returnByValue':True})['result']['value'],'Rows':rows}
 (root/'fonts.json').write_text(json.dumps(result,ensure_ascii=False,indent=2),encoding='utf-8');print(json.dumps(result,ensure_ascii=True))
 c.s.close()
finally:
 p.terminate()
 try:p.wait(timeout=5)
 except subprocess.TimeoutExpired:p.kill();p.wait()
`

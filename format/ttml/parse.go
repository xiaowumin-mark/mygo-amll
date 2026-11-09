package ttml

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	common "github.com/xiaowumin-mark/mygo-amll/format"
	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

const (
	ttNS     = "http://www.w3.org/ns/ttml"
	legacyNS = "http://www.w3.org/2006/10/ttaf1"
	ttmNS    = "http://www.w3.org/ns/ttml#metadata"
	xmlNS    = "http://www.w3.org/XML/1998/namespace"
	amllNS   = "http://www.example.com/ns/amll"
)

type node struct {
	name     xml.Name
	attrs    []xml.Attr
	parts    []part
	line     int
	column   int
	lang     string
	preserve bool
}

type part struct {
	text  string
	child *node
}

type frame struct {
	name     xml.Name
	node     *node
	lang     string
	preserve bool
	text     *strings.Builder
	metaName string
}

type documentParser struct {
	p        *formatutil.Parser
	lines    []formatutil.Line
	metadata []lyric.Property
	bgCount  int
}

// Parse builds an immutable lyric document. The caller must not mutate data
// during the call. The result does not retain the input byte slice. Cancellation
// is checked between XML tokens and while converting paragraph contents.
func Parse(ctx context.Context, data []byte, options common.Options) (common.Result, error) {
	p, err := formatutil.New(ctx, data, options)
	if err != nil {
		return common.Result{}, err
	}
	d := &documentParser{p: p}
	locator := sourceLocator{data: data, line: 1, column: 1}
	bomOffset := 0
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		bomOffset = 3
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if len(bytes.TrimSpace(data)) == 0 {
		return p.Finish(nil, nil, 0)
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	stack := make([]frame, 0, min(p.Options.Limits.MaxDepth, 16))
	var paragraph *node
	rootSeen := false
	rootClosed := false
	lastLine, lastColumn := 1, 1
	for {
		if err = p.Check(); err != nil {
			break
		}
		line, column := locator.at(int(decoder.InputOffset()) + bomOffset)
		lastLine, lastColumn = line, column
		var token xml.Token
		token, err = decoder.Token()
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			err = fmt.Errorf("ttml: line %d column %d: %w", line, column, err)
			break
		}
		switch t := token.(type) {
		case xml.StartElement:
			if len(stack) >= p.Options.Limits.MaxDepth {
				err = fmt.Errorf("ttml: line %d column %d: maximum XML depth exceeded", line, column)
				break
			}
			if len(stack) == 0 {
				if rootSeen || rootClosed || !isTT(t.Name, "tt") {
					err = fmt.Errorf("ttml: line %d column %d: require one tt root", line, column)
					break
				}
				rootSeen = true
			}
			f := frame{name: t.Name}
			if len(stack) > 0 {
				f.lang, f.preserve = stack[len(stack)-1].lang, stack[len(stack)-1].preserve
			}
			if lang, ok := attribute(t.Attr, xmlNS, "lang"); ok {
				f.lang = lang
			}
			if space, ok := attribute(t.Attr, xmlNS, "space"); ok {
				if space != "preserve" && space != "default" {
					err = fmt.Errorf("ttml: line %d column %d: invalid xml:space", line, column)
					break
				}
				f.preserve = space == "preserve"
			}
			if err = d.validateElement(t, line, column); err != nil {
				break
			}
			if role, _ := attributeEither(t.Attr, ttmNS, "role"); role == "x-bg" {
				for _, ancestor := range stack {
					if ancestor.node == nil {
						continue
					}
					if parentRole, _ := attributeEither(ancestor.node.attrs, ttmNS, "role"); parentRole == "x-bg" {
						err = formatutil.Error(line, column, "ttml", "nested x-bg roles are unsupported")
						break
					}
				}
				if err != nil {
					break
				}
			}
			if isTT(t.Name, "p") {
				if paragraph != nil {
					err = fmt.Errorf("ttml: line %d column %d: nested p is unsupported", line, column)
					break
				}
				if !hasAncestor(stack, "body") {
					err = fmt.Errorf("ttml: line %d column %d: p must be inside body", line, column)
					break
				}
				if err = p.AddLine(); err != nil {
					break
				}
			}
			if paragraph != nil || isTT(t.Name, "p") {
				f.node = &node{name: t.Name, attrs: t.Attr, line: line, column: column, lang: f.lang, preserve: f.preserve}
				if paragraph == nil {
					paragraph = f.node
				} else {
					parent := stack[len(stack)-1].node
					parent.parts = append(parent.parts, part{child: f.node})
				}
			} else if t.Name.Space == ttmNS && (t.Name.Local == "title" || t.Name.Local == "name") {
				f.metaName = t.Name.Local
				f.text = new(strings.Builder)
			} else if t.Name.Local == "meta" && (t.Name.Space == amllNS || t.Name.Space == "") {
				key, _ := attributeEither(t.Attr, amllNS, "key")
				value, ok := attributeEither(t.Attr, amllNS, "value")
				if key == "" || !ok {
					err = p.Warn(line, column, "metadata", "meta requires key and value")
				} else {
					d.metadata = append(d.metadata, lyric.Property{Name: key, Value: value})
				}
			}
			stack = append(stack, f)
		case xml.EndElement:
			if len(stack) == 0 {
				err = fmt.Errorf("ttml: unexpected closing element")
				break
			}
			f := &stack[len(stack)-1]
			if f.metaName != "" {
				d.metadata = append(d.metadata, lyric.Property{Name: f.metaName, Value: normalizeText(f.text.String(), false)})
			}
			if f.node == paragraph && paragraph != nil {
				err = d.convertParagraph(paragraph)
				paragraph = nil
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				rootClosed = true
			}
		case xml.CharData:
			if len(stack) > 0 {
				f := &stack[len(stack)-1]
				if f.node != nil {
					f.node.parts = append(f.node.parts, part{text: string(t)})
				} else if f.metaName != "" {
					f.text.Write(t)
				}
			} else if len(bytes.TrimSpace(t)) != 0 {
				err = fmt.Errorf("ttml: line %d column %d: text outside tt root", line, column)
			}
		case xml.Directive:
			err = fmt.Errorf("ttml: line %d column %d: XML directives and DTDs are unsupported", line, column)
		case xml.ProcInst:
			if t.Target != "xml" || rootSeen {
				err = fmt.Errorf("ttml: line %d column %d: processing instructions are unsupported", line, column)
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil && (!rootSeen || !rootClosed || len(stack) != 0) {
		err = fmt.Errorf("ttml: incomplete tt document")
	}
	if err != nil {
		var parseError *common.ParseError
		if !errors.As(err, &parseError) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			err = formatutil.Error(lastLine, lastColumn, "ttml", err.Error())
		}
		return common.Result{Diagnostics: p.Diagnostics}, err
	}
	return p.Finish(d.lines, d.metadata, 0)
}

// sourceLocator follows original bytes rather than decoder-normalized XML
// newlines, keeping CR, CRLF, BOM and multibyte columns in the common units.
type sourceLocator struct {
	data                 []byte
	offset, line, column int
	previousCR           bool
}

func (p *sourceLocator) at(target int) (int, int) {
	for p.offset < target {
		b := p.data[p.offset]
		p.offset++
		switch b {
		case '\r':
			p.line++
			p.column = 1
		case '\n':
			if !p.previousCR {
				p.line++
			}
			p.column = 1
		default:
			p.column++
		}
		p.previousCR = b == '\r'
	}
	return p.line, p.column
}

func (d *documentParser) validateElement(t xml.StartElement, line, column int) error {
	for _, attr := range t.Attr {
		if attr.Name.Space == "http://www.w3.org/ns/ttml#styling" && attr.Name.Local != "ruby" {
			if err := d.p.Warn(line, column, "unsupported-style", "TTML styling attributes are ignored"); err != nil {
				return err
			}
		}
		switch attr.Name.Local {
		case "src", "href":
			return fmt.Errorf("ttml: line %d column %d: resource references are unsupported", line, column)
		case "timeContainer":
			if attr.Value != "par" {
				return fmt.Errorf("ttml: line %d column %d: only parallel time containers are supported", line, column)
			}
		case "timeBase":
			if attr.Value != "media" {
				return fmt.Errorf("ttml: line %d column %d: only media time base is supported", line, column)
			}
		case "begin", "end", "dur":
			if isTT(t.Name, "body") || isTT(t.Name, "div") {
				return fmt.Errorf("ttml: line %d column %d: timing on body/div is outside the absolute lyric profile", line, column)
			}
		case "ruby", "style", "region":
			if err := d.p.Warn(line, column, "unsupported-style", "styling, layout and ruby attributes are ignored"); err != nil {
				return err
			}
		}
	}
	if isTT(t.Name, "tt") || isTT(t.Name, "head") || isTT(t.Name, "metadata") || isTT(t.Name, "body") || isTT(t.Name, "div") || isTT(t.Name, "p") || isTT(t.Name, "span") || isTT(t.Name, "br") {
		return nil
	}
	if t.Name.Space == ttmNS && (t.Name.Local == "agent" || t.Name.Local == "name" || t.Name.Local == "title") {
		return nil
	}
	if t.Name.Local == "meta" && (t.Name.Space == amllNS || t.Name.Space == "") {
		return nil
	}
	if t.Name.Local == "image" || t.Name.Local == "audio" || t.Name.Local == "video" {
		return fmt.Errorf("ttml: line %d column %d: media elements are unsupported", line, column)
	}
	return d.p.Warn(line, column, "unsupported-element", "unsupported element "+t.Name.Local+"; text content is retained")
}

func hasAncestor(stack []frame, local string) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		if isTT(stack[i].name, local) {
			return true
		}
	}
	return false
}

func isTT(name xml.Name, local string) bool {
	return name.Local == local && (name.Space == ttNS || name.Space == legacyNS || name.Space == "")
}

func attribute(attrs []xml.Attr, ns, local string) (string, bool) {
	for _, attr := range attrs {
		if attr.Name.Local == local && attr.Name.Space == ns {
			return attr.Value, true
		}
	}
	return "", false
}

func attributeEither(attrs []xml.Attr, ns, local string) (string, bool) {
	if value, ok := attribute(attrs, ns, local); ok {
		return value, true
	}
	return attribute(attrs, "", local)
}

func normalizeText(text string, preserve bool) string {
	if preserve {
		return text
	}
	return strings.Join(strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\r' || r == '\n'
	}), " ")
}

// parseTime deliberately avoids floating point so large or precise timestamps
// cannot silently round across a half-open interval boundary.
func parseTime(value string) (time.Duration, error) {
	if value == "" || strings.TrimSpace(value) != value {
		return 0, fmt.Errorf("invalid time %q", value)
	}
	if strings.Contains(value, ":") {
		parts := strings.Split(value, ":")
		if len(parts) != 2 && len(parts) != 3 {
			return 0, fmt.Errorf("unsupported clock %q", value)
		}
		var total time.Duration
		for i, part := range parts {
			unit := time.Second
			if i < len(parts)-1 {
				unit = time.Minute
				if len(parts) == 3 && i == 0 {
					unit = time.Hour
				}
				if strings.Contains(part, ".") {
					return 0, fmt.Errorf("invalid clock %q", value)
				}
			}
			term, err := decimalDuration(part, unit)
			if err != nil || (i > 0 && term >= 60*unit) || term > time.Duration(math.MaxInt64)-total {
				return 0, fmt.Errorf("invalid or overflowing clock %q", value)
			}
			total += term
		}
		return total, nil
	}
	for _, suffix := range []struct {
		text string
		unit time.Duration
	}{{"ms", time.Millisecond}, {"h", time.Hour}, {"m", time.Minute}, {"s", time.Second}} {
		if strings.HasSuffix(value, suffix.text) {
			return decimalDuration(strings.TrimSuffix(value, suffix.text), suffix.unit)
		}
	}
	return 0, fmt.Errorf("unsupported time %q (frame/tick units are unsupported)", value)
}

func decimalDuration(value string, unit time.Duration) (time.Duration, error) {
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, fmt.Errorf("invalid decimal time %q", value)
	}
	for _, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			return 0, fmt.Errorf("invalid decimal time %q", value)
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > math.MaxInt64/int64(unit) {
		return 0, fmt.Errorf("overflowing time %q", value)
	}
	result := whole * int64(unit)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 9 {
			return 0, fmt.Errorf("time requires 1..9 fractional digits")
		}
		fraction, _ := strconv.ParseInt(parts[1], 10, 64)
		denominator := int64(1)
		for range len(parts[1]) {
			denominator *= 10
		}
		// Divide before multiplying the remainder to avoid overflow for hours.
		frac := fraction*(int64(unit)/denominator) + fraction*(int64(unit)%denominator)/denominator
		if (fraction*(int64(unit)%denominator))%denominator != 0 || frac > math.MaxInt64-result {
			return 0, fmt.Errorf("time precision below a nanosecond or overflow")
		}
		result += frac
	}
	return time.Duration(result), nil
}

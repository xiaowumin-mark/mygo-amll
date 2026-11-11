package ttml

import (
	"context"
	"encoding/xml"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

func TestNamespaceDecoderMatchesExpandedXMLTokens(t *testing.T) {
	for _, data := range []string{
		`<root xmlns="urn:default" xmlns:p="urn:one" p:a="1" a="2"><p:child xmlns:p="urn:two" p:a="3"/><p:child/><child xmlns=""/></root>`,
		`<p:root p:a="value" xml:lang="en" xmlns:p="urn:prefix"><child xmlns="urn:default"/><p:child xmlns:xml="http://www.w3.org/XML/1998/namespace"/></p:root>`,
		`<root xmlns:a="urn:shared" xmlns:b="urn:shared" a:first="1" b:second="2"><a:item></a:item></root>`,
		`<?xml version="1.0"?><根 xmlns:字="urn:characters"><字:词 属性="text">content &amp; text</字:词><!--comment--></根>`,
	} {
		strict := newNamespaceDecoder(strings.NewReader(data), 0)
		standard := xml.NewDecoder(strings.NewReader(data))
		for {
			got, gotErr := strict.Token()
			want, wantErr := standard.Token()
			if !errors.Is(gotErr, wantErr) || !reflect.DeepEqual(got, want) {
				t.Fatalf("tokens differ for %q: got %#v, %v; want %#v, %v", data, got, gotErr, want, wantErr)
			}
			if strict.InputOffset() != standard.InputOffset() {
				t.Fatalf("input offsets differ: got %d want %d", strict.InputOffset(), standard.InputOffset())
			}
			if gotErr == io.EOF {
				break
			}
		}
	}
}

func TestNamespaceDecoderRejectsUnboundAndInvalidNamespaces(t *testing.T) {
	for _, test := range []struct {
		name string
		data string
	}{
		{"element prefix", `<root><missing:item/></root>`},
		{"attribute prefix", `<root missing:attribute="value"/>`},
		{"binding out of scope", `<root><child xmlns:p="urn:scope"/><p:item/></root>`},
		{"duplicate ordinary attribute", `<root a="one" a="two"/>`},
		{"duplicate declaration", `<root xmlns:p="urn:one" xmlns:p="urn:two"/>`},
		{"duplicate expanded attribute", `<root a:x="one" b:x="two" xmlns:a="urn:same" xmlns:b="urn:same"/>`},
		{"closing prefix mismatch", `<root xmlns:a="urn:same" xmlns:b="urn:same"><a:item></b:item></root>`},
		{"unexpected closing tag", `</root>`},
		{"unclosed element", `<root>`},
		{"rebound xml prefix", `<root xmlns:xml="urn:wrong"/>`},
		{"xml namespace alias", `<root xmlns:alias="http://www.w3.org/XML/1998/namespace"/>`},
		{"xml namespace default", `<root xmlns="http://www.w3.org/XML/1998/namespace"/>`},
		{"rebound xmlns prefix", `<root xmlns:xmlns="urn:wrong"/>`},
		{"xmlns namespace alias", `<root xmlns:a="http://www.w3.org/2000/xmlns/"/>`},
		{"xmlns namespace default", `<root xmlns="http://www.w3.org/2000/xmlns/"/>`},
		{"xmlns element prefix", `<xmlns:root/>`},
		{"empty prefixed namespace", `<root xmlns:p=""/>`},
		{"empty prefix part", `<:root/>`},
		{"empty local part", `<p: xmlns:p="urn:prefix"/>`},
		{"multiple colons", `<p:a:b xmlns:p="urn:prefix"/>`},
		{"invalid local start", `<p:1 xmlns:p="urn:prefix"/>`},
	} {
		t.Run(test.name, func(t *testing.T) {
			decoder := newNamespaceDecoder(strings.NewReader(test.data), 0)
			for {
				_, err := decoder.Token()
				if err == io.EOF {
					t.Fatalf("accepted namespace-invalid XML: %s", test.data)
				}
				if err != nil {
					break
				}
			}
		})
	}
}

func TestParseRejectsUnboundLyricsMetadataPrefix(t *testing.T) {
	data := `<tt><head><metadata><amll:meta key="musicName" value="synthetic"/></metadata></head><body><p begin="1s" end="2s">main</p></body></tt>`
	for _, strict := range []bool{false, true} {
		result, err := Parse(context.Background(), []byte(data), common.Options{Strict: strict})
		var parseError *common.ParseError
		if !errors.As(err, &parseError) || parseError.Code != "namespace" || !strings.Contains(parseError.Message, `unbound prefix "amll"`) || result.Document != nil {
			t.Fatalf("unbound lyric metadata prefix accepted with strict=%v: result=%+v error=%v", strict, result, err)
		}
	}
}

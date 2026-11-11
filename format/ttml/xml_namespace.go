package ttml

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	common "github.com/xiaowumin-mark/mygo-amll/format"
)

const xmlnsNamespace = "http://www.w3.org/2000/xmlns/"

// XMLDecl permits only version, optional encoding, then optional standalone.
// RawToken checks supported encodings but otherwise treats declarations as PI.
var xmlDeclaration = regexp.MustCompile(`^version[ \t\r\n]*=[ \t\r\n]*(?:"1\.0"|'1\.0')(?:[ \t\r\n]+encoding[ \t\r\n]*=[ \t\r\n]*(?:"[A-Za-z][A-Za-z0-9._-]*"|'[A-Za-z][A-Za-z0-9._-]*'))?(?:[ \t\r\n]+standalone[ \t\r\n]*=[ \t\r\n]*(?:"(?:yes|no)"|'(?:yes|no)'))?$`)

type namespaceBinding struct {
	prefix   string
	previous string
	existed  bool
}

type namespaceFrame struct {
	name     xml.Name // Original QName; closing tags must use the same prefix.
	bindings []namespaceBinding
}

// encoding/xml.Token deliberately accepts unbound namespace prefixes. A TTML
// document must instead be namespace-well-formed before any lyric conversion.
// RawToken retains QName prefixes so this wrapper can validate bindings and
// closing tags, then expose the same expanded names as encoding/xml.Token.
type namespaceDecoder struct {
	raw                    *xml.Decoder
	namespaces             map[string]string
	stack                  []namespaceFrame
	tokenLine, tokenColumn int
	initialColumnOffset    int
}

func newNamespaceDecoder(reader io.Reader, initialColumnOffset int) *namespaceDecoder {
	return &namespaceDecoder{raw: xml.NewDecoder(reader), namespaces: map[string]string{"xml": xmlNS}, initialColumnOffset: initialColumnOffset}
}

func (decoder *namespaceDecoder) InputOffset() int64 { return decoder.raw.InputOffset() }

func (decoder *namespaceDecoder) Token() (xml.Token, error) {
	decoder.tokenLine, decoder.tokenColumn = decoder.raw.InputPos()
	token, err := decoder.raw.RawToken()
	if err != nil {
		if err == io.EOF && len(decoder.stack) != 0 {
			return nil, decoder.inputError("namespace", "unexpected EOF inside %s", qualifiedXMLName(decoder.stack[len(decoder.stack)-1].name))
		}
		if err == io.EOF {
			return nil, err
		}
		return nil, decoder.inputError("xml", "%v", err)
	}
	switch value := token.(type) {
	case xml.ProcInst:
		if value.Target == "xml" && !xmlDeclaration.MatchString(strings.Trim(string(value.Inst), " \t\r\n")) {
			return nil, decoder.inputError("xml", "invalid XML declaration")
		}
		return value, nil
	case xml.StartElement:
		return decoder.start(value)
	case xml.EndElement:
		if len(decoder.stack) == 0 {
			return nil, decoder.inputError("namespace", "unexpected closing tag %s", qualifiedXMLName(value.Name))
		}
		frame := decoder.stack[len(decoder.stack)-1]
		if value.Name != frame.name {
			return nil, decoder.inputError("namespace", "closing tag %s does not match %s", qualifiedXMLName(value.Name), qualifiedXMLName(frame.name))
		}
		expanded, err := decoder.resolve(value.Name, true)
		if err != nil {
			return nil, err
		}
		decoder.restore(frame.bindings)
		decoder.stack = decoder.stack[:len(decoder.stack)-1]
		value.Name = expanded
		return value, nil
	default:
		return token, nil
	}
}

func (decoder *namespaceDecoder) start(value xml.StartElement) (xml.Token, error) {
	if !validXMLQName(value.Name) {
		return nil, decoder.inputError("namespace", "invalid QName %q", qualifiedXMLName(value.Name))
	}
	frame := namespaceFrame{name: value.Name}
	rawAttributes := make(map[xml.Name]struct{}, len(value.Attr))
	for _, attr := range value.Attr {
		if !validXMLQName(attr.Name) {
			decoder.restore(frame.bindings)
			return nil, decoder.inputError("namespace", "invalid attribute QName %q", qualifiedXMLName(attr.Name))
		}
		if _, exists := rawAttributes[attr.Name]; exists {
			decoder.restore(frame.bindings)
			return nil, decoder.inputError("namespace", "duplicate attribute %s", qualifiedXMLName(attr.Name))
		}
		rawAttributes[attr.Name] = struct{}{}
		prefix, declaration := namespaceDeclaration(attr.Name)
		if !declaration {
			continue
		}
		if err := validateNamespaceBinding(prefix, attr.Value); err != nil {
			decoder.restore(frame.bindings)
			return nil, decoder.inputError("namespace", "%v", err)
		}
		previous, existed := decoder.namespaces[prefix]
		frame.bindings = append(frame.bindings, namespaceBinding{prefix: prefix, previous: previous, existed: existed})
		decoder.namespaces[prefix] = attr.Value
	}
	name, err := decoder.resolve(value.Name, true)
	if err != nil {
		decoder.restore(frame.bindings)
		return nil, err
	}
	value.Name = name
	expandedAttributes := make(map[xml.Name]struct{}, len(value.Attr))
	for index := range value.Attr {
		attr := &value.Attr[index]
		if _, declaration := namespaceDeclaration(attr.Name); declaration {
			// Match encoding/xml.Token's representation of xmlns attributes.
			continue
		}
		name, err := decoder.resolve(attr.Name, false)
		if err != nil {
			decoder.restore(frame.bindings)
			return nil, err
		}
		if _, exists := expandedAttributes[name]; exists {
			decoder.restore(frame.bindings)
			return nil, decoder.inputError("namespace", "duplicate expanded attribute {%s}%s", name.Space, name.Local)
		}
		expandedAttributes[name] = struct{}{}
		attr.Name = name
	}
	decoder.stack = append(decoder.stack, frame)
	return value, nil
}

func namespaceDeclaration(name xml.Name) (string, bool) {
	if name.Space == "xmlns" {
		return name.Local, true
	}
	return "", name.Space == "" && name.Local == "xmlns"
}

func validateNamespaceBinding(prefix, namespace string) error {
	if prefix == "xmlns" || namespace == xmlnsNamespace {
		return fmt.Errorf("the xmlns prefix and namespace are reserved")
	}
	if prefix == "xml" && namespace != xmlNS || prefix != "xml" && namespace == xmlNS {
		return fmt.Errorf("the xml prefix must bind only to its reserved namespace")
	}
	if prefix != "" && namespace == "" {
		return fmt.Errorf("prefix %q cannot bind to an empty namespace", prefix)
	}
	return nil
}

func (decoder *namespaceDecoder) resolve(name xml.Name, element bool) (xml.Name, error) {
	prefix := name.Space
	if prefix == "xmlns" {
		return xml.Name{}, decoder.inputError("namespace", "xmlns cannot be used as an element prefix")
	}
	if prefix == "" && !element {
		return name, nil // A default namespace never applies to an attribute.
	}
	namespace, bound := decoder.namespaces[prefix]
	if prefix != "" && (!bound || namespace == "") {
		return xml.Name{}, decoder.inputError("namespace", "unbound prefix %q", prefix)
	}
	name.Space = namespace
	return name, nil
}

func (decoder *namespaceDecoder) inputError(code, message string, args ...any) error {
	column := decoder.tokenColumn
	if decoder.tokenLine == 1 {
		column += decoder.initialColumnOffset
	}
	return &common.ParseError{Diagnostic: common.Diagnostic{Code: code, Message: fmt.Sprintf(message, args...), Line: decoder.tokenLine, Column: column}}
}

func (decoder *namespaceDecoder) restore(bindings []namespaceBinding) {
	for index := len(bindings) - 1; index >= 0; index-- {
		binding := bindings[index]
		if binding.existed {
			decoder.namespaces[binding.prefix] = binding.previous
		} else {
			delete(decoder.namespaces, binding.prefix)
		}
	}
}

func qualifiedXMLName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

func validXMLQName(name xml.Name) bool {
	return validXMLNCName(name.Local) && (name.Space == "" || validXMLNCName(name.Space))
}

// XML 1.0 (fifth edition) NCName: NameStartChar/NameChar with ':' excluded.
// RawToken checks XML names lexically but does not enforce each QName part.
func validXMLNCName(value string) bool {
	if value == "" {
		return false
	}
	first := true
	for _, r := range value {
		start := r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' ||
			r >= 0xc0 && r <= 0xd6 || r >= 0xd8 && r <= 0xf6 || r >= 0xf8 && r <= 0x2ff ||
			r >= 0x370 && r <= 0x37d || r >= 0x37f && r <= 0x1fff || r >= 0x200c && r <= 0x200d ||
			r >= 0x2070 && r <= 0x218f || r >= 0x2c00 && r <= 0x2fef || r >= 0x3001 && r <= 0xd7ff ||
			r >= 0xf900 && r <= 0xfdcf || r >= 0xfdf0 && r <= 0xfffd || r >= 0x10000 && r <= 0xeffff
		if !start && (first || !(r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xb7 || r >= 0x300 && r <= 0x36f || r >= 0x203f && r <= 0x2040)) {
			return false
		}
		first = false
	}
	return true
}

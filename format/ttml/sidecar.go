package ttml

import (
	"encoding/xml"
	"math"
	"strings"
	"time"

	"github.com/xiaowumin-mark/mygo-amll/internal/formatutil"
	"github.com/xiaowumin-mark/mygo-amll/lyric"
)

// sidecarAnnotation is the single-language AMLL view of one Apple sidecar
// entry. The public lyric model selects the first language, as toAmllLyrics
// does when no preferred language is supplied.
type sidecarAnnotation struct {
	text  string
	words []lyric.Word
}

type sidecarEntry struct {
	translations    []sidecarAnnotation
	romanizations   []sidecarAnnotation
	bgTranslations  []sidecarAnnotation
	bgRomanizations []sidecarAnnotation
}

func normalizeITunesStart(start *xml.StartElement) {
	if start.Name.Space == itunesLegacyNS {
		start.Name.Space = itunesNS
	}
	for index := range start.Attr {
		if start.Attr[index].Name.Space == itunesLegacyNS {
			start.Attr[index].Name.Space = itunesNS
		}
	}
}

func isITunes(name xml.Name, local string) bool {
	return name.Local == local && (name.Space == itunesNS || name.Space == itunesLegacyNS || isTT(name, local))
}

func (d *documentParser) addMetadataUnique(name, value string) {
	if value == "" {
		return
	}
	if d.metadataSeen == nil {
		d.metadataSeen = make(map[lyric.Property]struct{})
	}
	// Metadata can also be appended by the ordinary head parser. Index only
	// entries added since the previous call, so all uniqueness checks together
	// remain linear in the number of metadata entries.
	for d.metadataIndexed < len(d.metadata) {
		if d.metadataIndexed&255 == 0 && d.p.Check() != nil {
			return
		}
		d.metadataSeen[d.metadata[d.metadataIndexed]] = struct{}{}
		d.metadataIndexed++
	}
	property := lyric.Property{Name: name, Value: value}
	if _, exists := d.metadataSeen[property]; exists {
		return
	}
	d.metadataSeen[property] = struct{}{}
	d.metadata = append(d.metadata, property)
	d.metadataIndexed++
}

func (d *documentParser) parseSidecar(root *node) error {
	if root == nil {
		return nil
	}
	songwriters, err := d.firstDescendant(root, "songwriters")
	if err != nil {
		return err
	}
	if songwriters != nil {
		writers, err := d.descendants(songwriters, "songwriter")
		if err != nil {
			return err
		}
		for _, writer := range writers {
			value, err := d.nodeText(writer)
			if err != nil {
				return err
			}
			d.addMetadataUnique("songwriters", strings.TrimFunc(value, ttmlWhitespace))
		}
	}
	audios, err := d.descendants(root, "audio")
	if err != nil {
		return err
	}
	for _, audio := range audios {
		if err := d.p.Check(); err != nil {
			return err
		}
		if value, ok := attributeEither(audio.attrs, itunesNS, "lyricOffset"); ok {
			d.addMetadataUnique("lyricOffset", strings.TrimFunc(value, ttmlWhitespace))
		}
		if value, ok := attributeEither(audio.attrs, itunesNS, "role"); ok {
			d.addMetadataUnique("audioRole", strings.TrimFunc(value, ttmlWhitespace))
		}
	}
	if err := d.parseSidecarEntries(root, "translations", "translation", false); err != nil {
		return err
	}
	return d.parseSidecarEntries(root, "transliterations", "transliteration", true)
}

func (d *documentParser) parseSidecarEntries(root *node, containerName, itemName string, romanization bool) error {
	container, err := d.firstDescendant(root, containerName)
	if err != nil {
		return err
	}
	if container == nil {
		return nil
	}
	items, err := d.descendants(container, itemName)
	if err != nil {
		return err
	}
	for _, item := range items {
		textNodes, err := d.descendants(item, "text")
		if err != nil {
			return err
		}
		for _, textNode := range textNodes {
			if err := d.p.Check(); err != nil {
				return err
			}
			forID, ok := attribute(textNode.attrs, "", "for")
			if !ok || forID == "" {
				continue
			}
			main, background, err := d.parseSidecarContent(textNode)
			if err != nil {
				return err
			}
			if d.sidecars == nil {
				d.sidecars = make(map[string]*sidecarEntry)
			}
			entry := d.sidecars[forID]
			if entry == nil {
				entry = new(sidecarEntry)
				d.sidecars[forID] = entry
			}
			if main != nil {
				if romanization {
					entry.romanizations = append(entry.romanizations, *main)
				} else {
					entry.translations = append(entry.translations, *main)
				}
			}
			if background != nil {
				if romanization {
					entry.bgRomanizations = append(entry.bgRomanizations, *background)
				} else {
					entry.bgTranslations = append(entry.bgTranslations, *background)
				}
			}
		}
	}
	return nil
}

func (d *documentParser) parseSidecarContent(root *node) (main, background *sidecarAnnotation, err error) {
	// Reuse the bounded paragraph walker without consuming the document's
	// background ID sequence or appending annotation-only lines.
	temporary := documentParser{p: d.p, appleWords: d.appleWords, annotationOnly: true}
	line := formatutil.Line{Line: lyric.Line{ID: "sidecar"}}
	var text textOutput
	var backgrounds []formatutil.Line
	if err := temporary.walk(root, &line, &text, &backgrounds); err != nil {
		return nil, nil, err
	}
	// The main lyric model stores trailing word spaces inline. AMLL sidecar
	// syllables instead expose clean text separately from endsWithSpace, and its
	// romanization matcher copies only that clean text.
	for index := range line.Words {
		if index&255 == 0 {
			if err := d.p.Check(); err != nil {
				return nil, nil, err
			}
		}
		line.Words[index].Text = strings.TrimFunc(line.Words[index].Text, ttmlWhitespace)
	}
	if value := text.text.String(); value != "" || len(line.Words) != 0 {
		main = &sidecarAnnotation{text: value, words: line.Words}
	}
	// AMLL's structured model has one background vocal. If malformed input has
	// several, the last DOM occurrence is the value retained by that parser.
	if len(backgrounds) != 0 {
		value := backgrounds[len(backgrounds)-1]
		for index := range value.Words {
			if index&255 == 0 {
				if err := d.p.Check(); err != nil {
					return nil, nil, err
				}
			}
			value.Words[index].Text = strings.TrimFunc(value.Words[index].Text, ttmlWhitespace)
		}
		if value.Text != "" || len(value.Words) != 0 {
			background = &sidecarAnnotation{text: value.Text, words: value.Words}
		}
	}
	return main, background, nil
}

func (d *documentParser) applySidecars() error {
	if len(d.sidecars) == 0 {
		return nil
	}
	mainLines := make(map[string]*formatutil.Line, len(d.lines))
	backgrounds := make(map[string]*formatutil.Line)
	for index := range d.lines {
		if index&255 == 0 {
			if err := d.p.Check(); err != nil {
				return err
			}
		}
		line := &d.lines[index]
		if line.ParentID != "" {
			if _, exists := backgrounds[line.ParentID]; !exists {
				backgrounds[line.ParentID] = line
			}
			continue
		}
		mainLines[line.ID] = line
	}
	for id, sidecar := range d.sidecars {
		if err := d.p.Check(); err != nil {
			return err
		}
		if line := mainLines[id]; line != nil {
			if err := d.applySidecarEntry(line, sidecar.translations, sidecar.romanizations); err != nil {
				return err
			}
		}
		if line := backgrounds[id]; line != nil {
			if err := d.applySidecarEntry(line, sidecar.bgTranslations, sidecar.bgRomanizations); err != nil {
				return err
			}
		}
	}
	return d.p.Check()
}

func (d *documentParser) applySidecarEntry(line *formatutil.Line, translations, romanizations []sidecarAnnotation) error {
	if line.Translation == "" && len(translations) != 0 {
		line.Translation = translations[0].text
	}
	if line.Romanization != "" || len(romanizations) == 0 {
		return nil
	}
	romanization := romanizations[0]
	if len(romanization.words) == 0 {
		line.Romanization = romanization.text
		return nil
	}
	return d.alignRomanization(line.Words, romanization.words)
}

// alignRomanization follows AMLL's fast start-time match and then its 10%
// interval-IoU fallback. Inputs remain in textual order.
func (d *documentParser) alignRomanization(words, romanWords []lyric.Word) error {
	searchStart := 0
	for index := range words {
		if err := d.p.Check(); err != nil {
			return err
		}
		word := &words[index]
		maxIOU := 0.0
		best := -1
		fast := false
		for candidate := searchStart; candidate < len(romanWords); candidate++ {
			if d.alignmentWork&255 == 0 {
				if err := d.p.Check(); err != nil {
					return err
				}
			}
			// A source-order matcher cannot binary-search imperfect timelines.
			// Bound its worst-case scan independently of context cancellation:
			// at most 32 candidate comparisons per permitted source segment.
			if d.alignmentWork/32 >= d.p.Options.Limits.MaxWords {
				return formatutil.Error(1, 1, "limit", "sidecar romanization alignment work limit exceeded")
			}
			d.alignmentWork++
			roman := romanWords[candidate]
			if absoluteDuration(word.Start-roman.Start) <= 2*time.Millisecond {
				word.Romanization = roman.Text
				searchStart = candidate + 1
				fast = true
				break
			}
			overlapStart := max(word.Start, roman.Start)
			overlapEnd := min(word.End, roman.End)
			intersection := max(time.Duration(0), overlapEnd-overlapStart)
			if intersection > 0 {
				unionStart := min(word.Start, roman.Start)
				unionEnd := max(word.End, roman.End)
				union := max(time.Duration(1), unionEnd-unionStart)
				iou := float64(intersection) / float64(union)
				if iou > maxIOU {
					maxIOU, best = iou, candidate
				}
			}
			if roman.Start >= word.End {
				break
			}
		}
		if !fast && best >= 0 && maxIOU >= 0.1 {
			word.Romanization = romanWords[best].Text
			searchStart = best + 1
		}
	}
	return nil
}

func absoluteDuration(value time.Duration) time.Duration {
	if value == time.Duration(math.MinInt64) {
		return time.Duration(math.MaxInt64)
	}
	if value < 0 {
		return -value
	}
	return value
}

func (d *documentParser) firstDescendant(root *node, local string) (*node, error) {
	var result *node
	err := d.visitDescendants(root, func(child *node) bool {
		if child.name.Local != local {
			return false
		}
		result = child
		return true
	})
	return result, err
}

func (d *documentParser) descendants(root *node, local string) ([]*node, error) {
	var result []*node
	err := d.visitDescendants(root, func(child *node) bool {
		if child.name.Local == local {
			result = append(result, child)
		}
		return false
	})
	return result, err
}

func (d *documentParser) visitDescendants(root *node, match func(*node) bool) error {
	var visit func(*node) (bool, error)
	visit = func(current *node) (bool, error) {
		if err := d.p.Check(); err != nil {
			return false, err
		}
		for index, piece := range current.parts {
			if index&255 == 0 {
				if err := d.p.Check(); err != nil {
					return false, err
				}
			}
			if piece.child == nil {
				continue
			}
			if match(piece.child) {
				return true, nil
			}
			if stopped, err := visit(piece.child); stopped || err != nil {
				return stopped, err
			}
		}
		return false, nil
	}
	_, err := visit(root)
	return err
}

func (d *documentParser) nodeText(root *node) (string, error) {
	var value strings.Builder
	var visit func(*node) error
	visit = func(current *node) error {
		if err := d.p.Check(); err != nil {
			return err
		}
		for index, piece := range current.parts {
			if index&255 == 0 {
				if err := d.p.Check(); err != nil {
					return err
				}
			}
			if piece.child == nil {
				value.WriteString(piece.text)
			} else if err := visit(piece.child); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(root)
	return value.String(), err
}

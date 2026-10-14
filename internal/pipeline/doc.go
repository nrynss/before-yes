package pipeline

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// segment is one labelled piece of a chunk. Ref is what the reader cites, so
// a finding can be checked against the doc it claims to quote.
type segment struct {
	Ref  string
	Doc  int // index into the docs slice
	Text string
}

// chunk is one reader call's input.
type chunk struct {
	Segments []segment
}

func (c chunk) size() int {
	n := 0
	for _, s := range c.Segments {
		n += len(s.Text)
	}
	return n
}

// Chunking bounds. Token Factory serves Nano with a 32k context and 4k of
// output tokens. A chunk of 16000 bytes is about 5000 tokens at the client's
// three bytes a token, which leaves room for the prompt and the findings.
const (
	chunkChars   = 16000
	overlapChars = 400
)

// chunkDocs packs docs into reader calls. Short docs, such as reviews, share
// a chunk. A long doc is split on paragraph or sentence boundaries with a
// small overlap, and each piece keeps the doc's ref, so a quote that crosses
// an edge still verifies against the whole doc.
func chunkDocs(docs []Doc) []chunk {
	var out []chunk
	var cur chunk
	flush := func() {
		if len(cur.Segments) > 0 {
			out = append(out, cur)
			cur = chunk{}
		}
	}
	for i, d := range docs {
		ref := refFor(d, i)
		text := strings.TrimSpace(d.Text)
		if text == "" {
			continue
		}
		if len(text) <= chunkChars {
			if cur.size()+len(text) > chunkChars {
				flush()
			}
			cur.Segments = append(cur.Segments, segment{Ref: ref, Doc: i, Text: text})
			continue
		}
		flush()
		for _, piece := range split(text, chunkChars, overlapChars) {
			out = append(out, chunk{Segments: []segment{{Ref: ref, Doc: i, Text: piece}}})
		}
	}
	flush()
	return out
}

func refFor(d Doc, i int) string {
	if d.Source.Kind == KindReview && d.Source.Index > 0 {
		return fmt.Sprintf("R%d", d.Source.Index)
	}
	return fmt.Sprintf("D%d", i+1)
}

// split cuts text into pieces of at most size bytes, preferring a paragraph
// break, then a sentence end, then a line break, then a space, and repeating
// about overlap bytes of the previous piece at the start of the next. Every
// cut falls on a rune boundary.
func split(text string, size, overlap int) []string {
	var out []string
	for len(text) > size {
		cut := best(text, size)
		out = append(out, text[:cut])
		start := cut - overlap
		if start < 0 {
			start = 0
		}
		if sp := strings.IndexByte(text[start:cut], ' '); sp >= 0 {
			start += sp + 1
		}
		for start < cut && !utf8.RuneStart(text[start]) {
			start++
		}
		text = text[start:]
	}
	if strings.TrimSpace(text) != "" {
		out = append(out, text)
	}
	return out
}

// best is where to cut text so the first piece is at most size bytes: after
// the last separator in its second half, or else at the last rune boundary.
func best(text string, size int) int {
	s := text[:size]
	half := size / 2
	for _, sep := range []string{"\n\n", ". ", "\n", " "} {
		if i := strings.LastIndex(s, sep); i >= half {
			return i + len(sep)
		}
	}
	n := size
	for n > 0 && !utf8.RuneStart(text[n]) {
		n--
	}
	if n == 0 {
		return size
	}
	return n
}

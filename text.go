package pngparse

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"strings"
)

// readText decodes every text chunk (tEXt, zTXt, iTXt) into TextEntry values,
// appending a problem/warning to the PNG state whenever a chunk is malformed.
func (p *PNG) readText() {
	for i := range p.Chunks {
		c := &p.Chunks[i]
		switch c.TypeString() {
		case TypeTEXT:
			p.readTextChunk(c)
		case TypeZTXT:
			p.readZtxtChunk(c)
		case TypeITXT:
			p.readItxtChunk(c)
		}
	}
}

// splitKeyword splits a text chunk payload on its first NUL. It returns the
// keyword and the remainder, and reports whether a NUL separator existed.
func splitKeyword(data []byte) (keyword, rest []byte, ok bool) {
	i := bytes.IndexByte(data, 0)
	if i < 0 {
		return nil, nil, false
	}
	return data[:i], data[i+1:], true
}

func (p *PNG) keywordProblems(kind string, keyword []byte, off int64) {
	if len(keyword) == 0 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"%s chunk at offset %d has an empty keyword", kind, off))
	}
	if len(keyword) > 79 {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"%s keyword at offset %d exceeds the 79-byte specification limit", kind, off))
	}
}

func (p *PNG) readTextChunk(c *Chunk) {
	keyword, text, ok := splitKeyword(c.Data)
	if !ok {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"tEXt chunk at offset %d has no NUL separating keyword and text", c.Offset))
		return
	}
	p.keywordProblems(TypeTEXT, keyword, c.Offset)
	if bytes.IndexByte(text, 0) >= 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"tEXt chunk at offset %d contains a NUL byte in its text", c.Offset))
	}
	p.TextInfo = append(p.TextInfo, TextEntry{
		Kind:    TypeTEXT,
		Keyword: latin1ToString(keyword),
		Text:    latin1ToString(text),
	})
}

func (p *PNG) readZtxtChunk(c *Chunk) {
	keyword, rest, ok := splitKeyword(c.Data)
	if !ok || len(rest) == 0 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"zTXt chunk at offset %d is missing the compression method byte", c.Offset))
		return
	}
	p.keywordProblems(TypeZTXT, keyword, c.Offset)
	method, compressed := rest[0], rest[1:]
	if method != 0 {
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"zTXt chunk at offset %d uses unknown compression method %d; skipping decompression",
			c.Offset, method))
		return
	}
	text, err := zlibDecompress(compressed)
	if err != nil {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"zTXt chunk at offset %d failed to decompress: %v", c.Offset, err))
		return
	}
	p.TextInfo = append(p.TextInfo, TextEntry{
		Kind:    TypeZTXT,
		Keyword: latin1ToString(keyword),
		Text:    latin1ToString(text),
	})
}

func (p *PNG) readItxtChunk(c *Chunk) {
	keyword, rest, ok := splitKeyword(c.Data)
	if !ok || len(rest) < 2 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"iTXt chunk at offset %d is too short to carry compression flag and method", c.Offset))
		return
	}
	p.keywordProblems(TypeITXT, keyword, c.Offset)
	flag, method := rest[0], rest[1]
	tail := rest[2:]

	// Language tag: NUL-terminated.
	lang, tail2, ok := splitKeyword(tail)
	if !ok {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"iTXt chunk at offset %d is missing the language tag NUL terminator", c.Offset))
		return
	}
	// Translated keyword: NUL-terminated.
	_, tail3, ok := splitKeyword(tail2)
	if !ok {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"iTXt chunk at offset %d is missing the translated keyword NUL terminator", c.Offset))
		return
	}

	var text []byte
	switch {
	case flag == 1:
		if method != 0 {
			p.Warnings = append(p.Warnings, fmt.Sprintf(
				"iTXt chunk at offset %d uses unknown compression method %d; skipping decompression",
				c.Offset, method))
			return
		}
		dec, err := zlibDecompress(tail3)
		if err != nil {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"iTXt chunk at offset %d failed to decompress: %v", c.Offset, err))
			return
		}
		text = dec
	case flag == 0:
		if method != 0 {
			p.Warnings = append(p.Warnings, fmt.Sprintf(
				"iTXt chunk at offset %d has a nonzero compression method with an uncompressed flag", c.Offset))
		}
		text = tail3
	default:
		p.Warnings = append(p.Warnings, fmt.Sprintf(
			"iTXt chunk at offset %d has invalid compression flag %d", c.Offset, flag))
		return
	}

	_ = lang // recorded for future use; text is UTF-8
	p.TextInfo = append(p.TextInfo, TextEntry{
		Kind:    TypeITXT,
		Keyword: latin1ToString(keyword),
		Text:    string(text),
	})
}

// zlibDecompress decompresses a zlib (RFC 1950) stream. It closes the reader
// and wraps a zip of value/context errors into one readable cause.
func zlibDecompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, io.ErrUnexpectedEOF
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

// latin1ToString decodes ISO-8859-1 bytes to a Go string by widening each
// byte to its Unicode code point.
func latin1ToString(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, c := range b {
		sb.WriteRune(rune(c))
	}
	return sb.String()
}

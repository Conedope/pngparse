package pngparse

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Standard chunk type names.
const (
	TypeIHDR = "IHDR"
	TypePLTE = "PLTE"
	TypeIDAT = "IDAT"
	TypeIEND = "IEND"
	TypeTEXT = "tEXt"
	TypeZTXT = "zTXt"
	TypeITXT = "iTXt"
)

// IHDR holds the 13 decoded bytes of the IHDR chunk.
type IHDR struct {
	Width, Height uint32
	BitDepth      byte
	ColorType     byte
	Compression   byte
	Filter        byte
	Interlace     byte
}

// TextEntry is one decoded text chunk (tEXt, zTXt or iTXt).
type TextEntry struct {
	Kind    string // chunk type: tEXt, zTXt or iTXt
	Keyword string
	Text    string
}

// PNG is the result of parsing and inspecting a whole PNG byte stream.
type PNG struct {
	Chunks   []Chunk
	Header   IHDR
	TextInfo []TextEntry
	Problems []string
	Warnings []string

	hasHeader bool // set once a well-formed 13-byte IHDR payload was decoded
}

// ColorTypeName maps a PNG color type code to its specification name.
func ColorTypeName(ct byte) string {
	switch ct {
	case 0:
		return "grayscale"
	case 2:
		return "truecolor (RGB)"
	case 3:
		return "indexed-color"
	case 4:
		return "grayscale with alpha"
	case 6:
		return "truecolor with alpha (RGBA)"
	default:
		return "unknown"
	}
}

// InterlaceName maps an IHDR interlace code to its specification name.
func InterlaceName(i byte) string {
	switch i {
	case 0:
		return "none"
	case 1:
		return "Adam7"
	default:
		return "unknown"
	}
}

// BitDepthAllowed reports whether bit depth bit is legal for the given PNG
// color type, per Table 11.1 of the PNG specification:
//
//	0 (grayscale):            1, 2, 4, 8, 16
//	2 (truecolor):            8, 16
//	3 (indexed-color):        1, 2, 4, 8
//	4 (grayscale + alpha):    8, 16
//	6 (truecolor + alpha):    8, 16
func BitDepthAllowed(color, bit byte) bool {
	switch color {
	case 0:
		return bit == 1 || bit == 2 || bit == 4 || bit == 8 || bit == 16
	case 2, 4, 6:
		return bit == 8 || bit == 16
	case 3:
		return bit == 1 || bit == 2 || bit == 4 || bit == 8
	}
	return false
}

// Parse validates the signature, walks every chunk, verifies CRCs, decodes
// the IHDR and text chunks, and enforces the PNG structural rules. Semantic
// violations are collected in the returned PNG's Problems/Warnings; the
// returned error is reserved for non-PNG input and unrecoverable structural
// parse failures (truncation, corrupt chunk type).
func Parse(data []byte) (*PNG, error) {
	chunks, err := ParseChunks(data)
	if err != nil {
		return nil, err
	}
	p := &PNG{Chunks: chunks}
	p.checkHeader()
	p.checkPalette()
	p.checkImageData()
	p.checkEnd()
	p.readText()
	p.checkCRCs()
	return p, nil
}

// ParseFile reads path and runs Parse on its contents.
func ParseFile(path string) (*PNG, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

// Valid returns every recorded problem as an error.
func (p *PNG) Valid() []error {
	errs := make([]error, 0, len(p.Problems))
	for _, pr := range p.Problems {
		errs = append(errs, errors.New(pr))
	}
	return errs
}

// CriticalChunks returns the critical chunks (IHDR, PLTE, IDAT, IEND and any
// other chunk whose type starts with an uppercase letter) in file order.
func (p *PNG) CriticalChunks() []Chunk {
	var out []Chunk
	for _, c := range p.Chunks {
		if c.IsCritical() {
			out = append(out, c)
		}
	}
	return out
}

func (p *PNG) checkCRCs() {
	for i := range p.Chunks {
		if !p.Chunks[i].CrcValid {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"chunk %q at offset %d has invalid CRC (stored %08x)",
				p.Chunks[i].TypeString(), p.Chunks[i].Offset, p.Chunks[i].CrcValue))
		}
	}
}

// checkHeader validates the IHDR presence, uniqueness, ordering and fields.
func (p *PNG) checkHeader() {
	ihdrs := FindChunks(p.Chunks, TypeIHDR)
	switch len(ihdrs) {
	case 0:
		p.Problems = append(p.Problems, "missing required IHDR chunk")
		return
	case 1:
	default:
		p.Problems = append(p.Problems, fmt.Sprintf("single IHDR chunk required, found %d", len(ihdrs)))
	}
	if len(p.Chunks) == 0 || p.Chunks[0].TypeString() != TypeIHDR {
		p.Problems = append(p.Problems, "IHDR must be the first chunk")
	}
	ihdr := ihdrs[0]
	if ihdr.Length != 13 {
		p.Problems = append(p.Problems, fmt.Sprintf("IHDR length must be 13, found %d", ihdr.Length))
		return
	}
	d := ihdr.Data
	h := IHDR{
		Width:       binary.BigEndian.Uint32(d[0:4]),
		Height:      binary.BigEndian.Uint32(d[4:8]),
		BitDepth:    d[8],
		ColorType:   d[9],
		Compression: d[10],
		Filter:      d[11],
		Interlace:   d[12],
	}
	p.Header = h
	p.hasHeader = true

	if h.Width == 0 {
		p.Problems = append(p.Problems, "IHDR width must be greater than zero")
	}
	if h.Height == 0 {
		p.Problems = append(p.Problems, "IHDR height must be greater than zero")
	}
	switch h.ColorType {
	case 0, 2, 3, 4, 6:
		if !BitDepthAllowed(h.ColorType, h.BitDepth) {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"bit depth %d is not allowed for color type %d (%s)",
				h.BitDepth, h.ColorType, ColorTypeName(h.ColorType)))
		}
	default:
		p.Problems = append(p.Problems, fmt.Sprintf(
			"invalid color type %d (valid color types are 0, 2, 3, 4 and 6)", h.ColorType))
	}
	if h.Compression != 0 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"invalid compression method %d (only 0 is defined)", h.Compression))
	}
	if h.Filter != 0 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"invalid filter method %d (only 0 is defined)", h.Filter))
	}
	if h.Interlace != 0 && h.Interlace != 1 {
		p.Problems = append(p.Problems, fmt.Sprintf(
			"invalid interlace method %d (only 0 and 1 are defined)", h.Interlace))
	}
}

func (p *PNG) checkPalette() {
	pltes := FindChunks(p.Chunks, TypePLTE)
	if len(pltes) > 1 {
		p.Problems = append(p.Problems, fmt.Sprintf("single PLTE chunk allowed, found %d", len(pltes)))
	}
	idatIdx := firstIDATIndex(p.Chunks)
	for i, c := range p.Chunks {
		if c.TypeString() != TypePLTE {
			continue
		}
		if c.Length%3 != 0 {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"PLTE length must be divisible by 3 (each entry is 3 bytes), found %d", c.Length))
		}
		if idatIdx >= 0 && i > idatIdx {
			p.Problems = append(p.Problems, "PLTE must appear before the first IDAT chunk")
		}
		if p.hasHeader && (p.Header.ColorType == 0 || p.Header.ColorType == 4) {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"PLTE chunk is not allowed for color type %d (%s)",
				p.Header.ColorType, ColorTypeName(p.Header.ColorType)))
		}
	}
	if p.hasHeader && p.Header.ColorType == 3 {
		if len(pltes) == 0 {
			p.Problems = append(p.Problems, "indexed-color image requires a PLTE chunk")
		} else {
			entries := len(pltes[0].Data) / 3
			max := 1 << p.Header.BitDepth
			if entries > max {
				p.Problems = append(p.Problems, fmt.Sprintf(
					"PLTE has %d entries but bit depth %d allows at most %d",
					entries, p.Header.BitDepth, max))
			}
		}
	}
}

func (p *PNG) checkImageData() {
	idats := FindChunks(p.Chunks, TypeIDAT)
	if len(idats) == 0 {
		p.Problems = append(p.Problems, "image contains no IDAT chunk")
		return
	}
	if !chunksContiguous(p.Chunks, TypeIDAT) {
		p.Warnings = append(p.Warnings,
			"IDAT chunks are not contiguous (another chunk appears between IDAT chunks)")
	}
	var total uint64
	for _, c := range idats {
		total += uint64(c.Length)
	}
	if total == 0 {
		p.Warnings = append(p.Warnings, "IDAT chunks carry zero compressed bytes")
	}
}

func (p *PNG) checkEnd() {
	iends := FindChunks(p.Chunks, TypeIEND)
	if len(iends) == 0 {
		p.Problems = append(p.Problems, "missing required IEND chunk")
		return
	}
	if len(iends) > 1 {
		p.Problems = append(p.Problems, fmt.Sprintf("single IEND chunk allowed, found %d", len(iends)))
	}
	if len(p.Chunks) > 0 && p.Chunks[len(p.Chunks)-1].TypeString() != TypeIEND {
		p.Problems = append(p.Problems, "IEND must be the last chunk")
	}
	for i := range iends {
		if iends[i].Length != 0 {
			p.Problems = append(p.Problems, fmt.Sprintf(
				"IEND chunk length must be 0, found %d", iends[i].Length))
		}
	}
}

// Describe renders a human-readable inspection report of the parsed PNG.
func Describe(p *PNG) string {
	var b strings.Builder

	b.WriteString("Signature: OK\n")
	b.WriteString("\nChunks\n")
	b.WriteString("  offset  type      len     crc\n")
	for _, c := range p.Chunks {
		status := "OK"
		if !c.CrcValid {
			status = "BAD"
		}
		fmt.Fprintf(&b, "  %6d  %-8s %6d  %s\n", c.Offset, c.TypeString(), c.Length, status)
	}

	if p.hasHeader {
		b.WriteString("\nHeader\n")
		fmt.Fprintf(&b, "  width:       %d\n", p.Header.Width)
		fmt.Fprintf(&b, "  height:      %d\n", p.Header.Height)
		fmt.Fprintf(&b, "  bit depth:   %d\n", p.Header.BitDepth)
		fmt.Fprintf(&b, "  color type:  %d (%s)\n", p.Header.ColorType, ColorTypeName(p.Header.ColorType))
		fmt.Fprintf(&b, "  compression: %d\n", p.Header.Compression)
		fmt.Fprintf(&b, "  filter:      %d\n", p.Header.Filter)
		fmt.Fprintf(&b, "  interlace:   %d (%s)\n", p.Header.Interlace, InterlaceName(p.Header.Interlace))
	}

	var critical []string
	for _, c := range p.CriticalChunks() {
		critical = append(critical, c.TypeString())
	}
	if len(critical) > 0 {
		b.WriteString("\nCritical chunks\n")
		b.WriteString("  " + strings.Join(critical, " ") + "\n")
	}

	if len(p.TextInfo) > 0 {
		b.WriteString("\nText\n")
		for _, t := range p.TextInfo {
			fmt.Fprintf(&b, "  [%s] %s: %s\n", t.Kind, t.Keyword, t.Text)
		}
	}

	if len(p.Warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, w := range p.Warnings {
			fmt.Fprintf(&b, "  - %s\n", w)
		}
	}

	if len(p.Problems) > 0 {
		b.WriteString("\nProblems\n")
		for _, pr := range p.Problems {
			fmt.Fprintf(&b, "  - %s\n", pr)
		}
	} else {
		b.WriteString("\nValidation: ok\n")
	}
	return b.String()
}

// firstIDATIndex returns the index of the first IDAT chunk, or -1.
func firstIDATIndex(chunks []Chunk) int {
	for i, c := range chunks {
		if c.TypeString() == TypeIDAT {
			return i
		}
	}
	return -1
}

// chunksContiguous reports whether all chunks of the given type form one
// uninterrupted run (no other chunk type in between).
func chunksContiguous(chunks []Chunk, typ string) bool {
	first, last := -1, -1
	for i, c := range chunks {
		if c.TypeString() == typ {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return true
	}
	for i := first; i <= last; i++ {
		if chunks[i].TypeString() != typ {
			return false
		}
	}
	return true
}

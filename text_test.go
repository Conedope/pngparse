package pngparse

import (
	"bytes"
	"strings"
	"testing"
)

func TestTEXTLatin1RoundTrip(t *testing.T) {
	keyword := []byte("Autor")
	// Latin-1 (ISO-8859-1) bytes: "Hello üß é µ".
	text := []byte{'H', 'e', 'l', 'l', 'o', ' ', 0xfc, 0xdf, ' ', 0xe9, ' ', 0xb5}

	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("tEXt", append(append([]byte{}, keyword...), append([]byte{0x00}, text...)...)),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.TextInfo) != 1 {
		t.Fatalf("TextInfo = %d entries, want 1", len(p.TextInfo))
	}
	e := p.TextInfo[0]
	if e.Kind != "tEXt" {
		t.Errorf("kind = %q, want tEXt", e.Kind)
	}
	if e.Keyword != "Autor" {
		t.Errorf("keyword = %q, want Autor", e.Keyword)
	}
	want := "Hello \u00fc\u00df \u00e9 \u00b5"
	if e.Text != want {
		t.Errorf("text = %q, want %q", e.Text, want)
	}
	// Latin-1 round trip: encoding the widened string back to bytes must
	// reproduce the original single-byte payload exactly.
	if got := toLatin1(e.Text); !bytes.Equal(got, text) {
		t.Errorf("latin-1 round trip failed: %v != %v", got, text)
	}
}

// toLatin1 re-encodes a string of code points in the Latin-1 range back to
// single bytes, inverting latin1ToString.
func toLatin1(s string) []byte {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			panic("not latin-1")
		}
		out = append(out, byte(r))
	}
	return out
}

func TestZTXTRoundTrip(t *testing.T) {
	raw := []byte("compressed comment ")
	raw = append(raw, 0xfc, 0xdf, ' ', 0xe9, 0xb5) // latin-1, twice
	raw = append(raw, []byte(", twice, twice, twice")...)
	payload := append([]byte("Comment\x00"), 0x00)
	payload = append(payload, testCompress(raw)...)

	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("zTXt", payload),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.TextInfo) != 1 {
		t.Fatalf("TextInfo = %d entries, want 1", len(p.TextInfo))
	}
	e := p.TextInfo[0]
	if e.Kind != "zTXt" || e.Keyword != "Comment" {
		t.Errorf("entry = %+v", e)
	}
	want := "compressed comment \u00fc\u00df \u00e9\u00b5, twice, twice, twice"
	if e.Text != want {
		t.Errorf("zTXt decompressed text = %q, want %q", e.Text, want)
	}
	if got := toLatin1(e.Text); !bytes.Equal(got, raw) {
		t.Errorf("zTXt latin-1 round trip failed: %v != %v", got, raw)
	}
}

func TestZTXTBadMethod(t *testing.T) {
	payload := []byte("Comment\x00\x07not-zlib-data-here")
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("zTXt", payload),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(p, "unknown compression method") {
		t.Errorf("warnings = %v", p.Warnings)
	}
	if len(p.TextInfo) != 0 {
		t.Errorf("TextInfo = %d entries, want 0 (skipped)", len(p.TextInfo))
	}
}

func TestITXTUncompressed(t *testing.T) {
	// keyword\0 flag=0 method=0 lang\0 translated\0 UTF-8 text
	payload := []byte("Title\x00\x00\x00en\x00\xde\x9f\x9cbergesetz\x00Hello, UTF-8 \xc3\xa9\xe2\x98\x83")
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("iTXt", payload),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.TextInfo) != 1 {
		t.Fatalf("TextInfo = %d entries, want 1", len(p.TextInfo))
	}
	e := p.TextInfo[0]
	if e.Kind != "iTXt" || e.Keyword != "Title" {
		t.Errorf("entry = %+v", e)
	}
	if e.Text != "Hello, UTF-8 \u00e9\u2603" {
		t.Errorf("iTXt text = %q", e.Text)
	}
}

func TestITXTCompressed(t *testing.T) {
	secret := []byte("compressed iTXt payload with \u00e9 and \u2603 and padding padding padding")
	payload := []byte("Secret\x00\x01\x00en\x00translated\x00")
	payload = append(payload, testCompress(secret)...)
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("iTXt", payload),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.TextInfo) != 1 {
		t.Fatalf("TextInfo = %d entries, want 1 (problems=%v warnings=%v)", len(p.TextInfo), p.Problems, p.Warnings)
	}
	if p.TextInfo[0].Keyword != "Secret" || p.TextInfo[0].Text != string(secret) {
		t.Errorf("entry = %+v", p.TextInfo[0])
	}
}

func TestITXTBadMethod(t *testing.T) {
	payload := []byte("Kw\x00\x01\x09\x00\x00")
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("iTXt", payload),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(p, "unknown compression method") {
		t.Errorf("warnings = %v", p.Warnings)
	}
}

func TestTextMalformed(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    string
	}{
		{"tEXt missing separator", []byte("kwandtext"), "no NUL separating keyword and text"},
		{"tEXt empty keyword", []byte("\x00text"), "empty keyword"},
		{"tEXt NUL in text", []byte("kw\x00a\x00b"), "contains a NUL byte"},
		{"zTXt missing method", []byte("kw\x00"), "missing the compression method"},
		{"iTXt too short", []byte("kw\x00\x00"), "too short"},
		{"iTXt missing language NUL", []byte("kw\x00\x00\x00lang"), "language tag NUL terminator"},
	}
	for _, tc := range cases {
		var chunkBytes []byte
		switch tc.name {
		case "zTXt missing method":
			chunkBytes = testChunk("zTXt", tc.payload)
		case "iTXt too short", "iTXt missing language NUL":
			chunkBytes = testChunk("iTXt", tc.payload)
		default:
			chunkBytes = testChunk("tEXt", tc.payload)
		}
		pngb := testPNG(testIHDR(1, 1, 8, 2), chunkBytes,
			testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil))
		p, err := Parse(pngb)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !hasProblem(p, tc.want) && !hasWarning(p, tc.want) {
			t.Errorf("%s: problems=%v warnings=%v, want %q",
				tc.name, p.Problems, p.Warnings, tc.want)
		}
	}
}

func TestCorruptCRCTextChunk(t *testing.T) {
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("tEXt", []byte("Key\x00value")),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	// The tEXt chunk starts at offset 33; its data occupies 41..49 and the
	// CRC follows at 50. Corrupt a payload byte so only that CRC fails.
	bad := flipByte(pngb, 42)
	p, err := Parse(bad)
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "invalid CRC") {
		t.Errorf("problems = %v", p.Problems)
	}
	for i := range p.Chunks {
		c := &p.Chunks[i]
		if c.TypeString() == "tEXt" && c.CrcValid {
			t.Error("corrupted tEXt chunk should report CrcValid=false")
		}
	}
	// We still capture the text entry despite the CRC failure.
	if len(p.TextInfo) != 1 {
		t.Errorf("TextInfo = %d entries, want 1", len(p.TextInfo))
	}
}

func TestMultiTextEntries(t *testing.T) {
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("tEXt", []byte("A\x00one")),
		testChunk("zTXt", append([]byte("B\x00\x00"), testCompress([]byte("two"))...)),
		testChunk("iTXt", []byte("C\x00\x00\x00en\x00\x00three")),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.TextInfo) != 3 {
		t.Fatalf("TextInfo = %d entries, want 3", len(p.TextInfo))
	}
	var got []string
	for _, e := range p.TextInfo {
		got = append(got, e.Kind+":"+e.Keyword+"="+e.Text)
	}
	want := "tEXt:A=one,zTXt:B=two,iTXt:C=three"
	if strings.Join(got, ",") != want {
		t.Errorf("entries = %v, want %s", got, want)
	}
}

package pngparse

import (
	"strings"
	"testing"
)

// TestBitDepthMatrix enumerates every valid (color type, bit depth) pair and
// checks the spec rules: 0:{1,2,4,8,16}, 2:{8,16}, 3:{1,2,4,8}, 4:{8,16},
// 6:{8,16}. Colors outside 0/2/3/4/6 are tested separately.
func TestBitDepthMatrix(t *testing.T) {
	valid := map[byte][]byte{
		0: {1, 2, 4, 8, 16},
		2: {8, 16},
		3: {1, 2, 4, 8},
		4: {8, 16},
		6: {8, 16},
	}
	full := []byte{1, 2, 4, 8, 16}
	for _, color := range []byte{0, 2, 3, 4, 6} {
		for _, bit := range full {
			pngb := testPNG(
				testIHDR(1, 1, bit, color),
				testChunk("IDAT", []byte{0x00}),
				testChunk("IEND", nil),
			)
			p, err := Parse(pngb)
			if err != nil {
				t.Fatalf("color %d bit %d: %v", color, bit, err)
			}
			got := hasProblem(p, "bit depth")
			want := true
			for _, allowed := range valid[color] {
				if allowed == bit {
					want = false
				}
			}
			if got != want {
				t.Errorf("color %d bit %d: bit-depth problem = %v, want %v (problems: %v)",
					color, bit, got, want, p.Problems)
			}
		}
	}
}

func TestInvalidColorTypes(t *testing.T) {
	for _, color := range []byte{1, 5, 7} {
		pngb := testPNG(
			testIHDR(1, 1, 8, color),
			testChunk("IDAT", []byte{0x00}),
			testChunk("IEND", nil),
		)
		p, err := Parse(pngb)
		if err != nil {
			t.Fatal(err)
		}
		if !hasProblem(p, "invalid color type") {
			t.Errorf("color %d: problems = %v", color, p.Problems)
		}
	}
}

func TestValidIHDRMinimalRGB(t *testing.T) {
	p, err := Parse(testMinimalPNG())
	if err != nil {
		t.Fatal(err)
	}
	noProblems(t, p)
	if len(p.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", p.Warnings)
	}
	if p.Header.Width != 1 || p.Header.Height != 1 {
		t.Errorf("header = %+v, want 1x1", p.Header)
	}
	if p.Header.BitDepth != 8 || p.Header.ColorType != 2 {
		t.Errorf("header = %+v, want bit 8 color 2", p.Header)
	}
}

func TestTwoIHDRs(t *testing.T) {
	pngb := testPNG(testIHDR(1, 1, 8, 2), testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil))
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "single IHDR chunk required, found 2") {
		t.Errorf("problems = %v", p.Problems)
	}
}

func TestIHDRNotFirst(t *testing.T) {
	pngb := testPNG(testChunk("tEXt", []byte("a\x00b")),
		testIHDR(1, 1, 8, 2), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil))
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "IHDR must be the first chunk") {
		t.Errorf("problems = %v", p.Problems)
	}
}

func TestZeroDimensions(t *testing.T) {
	for name, dims := range map[string]func() []byte{
		"zero width": func() []byte {
			return testPNG(testIHDR(0, 1, 8, 2), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil))
		},
		"zero height": func() []byte {
			return testPNG(testIHDR(1, 0, 8, 2), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil))
		},
	} {
		p, err := Parse(dims())
		if err != nil {
			t.Fatal(err)
		}
		if !hasProblem(p, "greater than zero") {
			t.Errorf("%s: problems = %v", name, p.Problems)
		}
	}
}

func TestInvalidIHDRFields(t *testing.T) {
	cases := []struct {
		name string
		pngb []byte
		want string
	}{
		{"bad compression", testPNG(testIHDRFull(1, 1, 8, 2, 1, 0, 0), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)), "compression method"},
		{"bad filter", testPNG(testIHDRFull(1, 1, 8, 2, 0, 3, 0), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)), "filter method"},
		{"bad interlace", testPNG(testIHDRFull(1, 1, 8, 2, 0, 0, 2), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)), "interlace method"},
		{"bad color type", testPNG(testIHDR(1, 1, 8, 5), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)), "invalid color type 5"},
		{"short IHDR", testPNG(testChunk("IHDR", make([]byte, 12)), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)), "IHDR length must be 13"},
	}
	for _, tc := range cases {
		p, err := Parse(tc.pngb)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !hasProblem(p, tc.want) {
			t.Errorf("%s: problems = %v, want containing %q", tc.name, p.Problems, tc.want)
		}
	}

	// Adam7 interlace is valid and must not raise a problem.
	p, err := Parse(testPNG(testIHDRFull(1, 1, 8, 2, 0, 0, 1), testChunk("IDAT", []byte{0x00}), testChunk("IEND", nil)))
	if err != nil {
		t.Fatal(err)
	}
	if hasProblem(p, "interlace") {
		t.Errorf("Adam7 should be valid, problems = %v", p.Problems)
	}
}

func TestPLTERules(t *testing.T) {
	// PLTE length must be divisible by 3 (here 5 bytes).
	p, err := Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("PLTE", make([]byte, 5)),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "divisible by 3") {
		t.Errorf("problems = %v", p.Problems)
	}

	// A well-formed 6-byte PLTE on truecolor is legal.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("PLTE", make([]byte, 6)),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if hasProblem(p, "PLTE") {
		t.Errorf("legal PLTE flagged: %v", p.Problems)
	}

	// Indexed color requires a PLTE.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 3),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "requires a PLTE chunk") {
		t.Errorf("problems = %v", p.Problems)
	}

	// Indexed color with too many palette entries for bit depth 1 (max 2).
	p, err = Parse(testPNG(
		testIHDR(1, 1, 1, 3),
		testChunk("PLTE", make([]byte, 12)), // 4 entries
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "allows at most 2") {
		t.Errorf("problems = %v", p.Problems)
	}

	// PLTE must precede the first IDAT.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x00}),
		testChunk("PLTE", make([]byte, 6)),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "before the first IDAT") {
		t.Errorf("problems = %v", p.Problems)
	}

	// Duplicate PLTE.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("PLTE", make([]byte, 6)),
		testChunk("PLTE", make([]byte, 6)),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "single PLTE chunk allowed, found 2") {
		t.Errorf("problems = %v", p.Problems)
	}

	// PLTE forbidden for grayscale (color type 0).
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 0),
		testChunk("PLTE", make([]byte, 6)),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "not allowed for color type 0") {
		t.Errorf("problems = %v", p.Problems)
	}
}

func TestIDATContiguityAndPresence(t *testing.T) {
	// IDAT split by an ancillary tEXt produces a warning, not a fatal error.
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x01}),
		testChunk("tEXt", []byte("note\x00between")),
		testChunk("IDAT", []byte{0x02}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if !hasWarning(p, "not contiguous") {
		t.Errorf("warnings = %v", p.Warnings)
	}
	if len(p.Problems) != 0 {
		t.Errorf("split IDAT should only warn, problems = %v", p.Problems)
	}

	// No IDAT at all.
	p, err = Parse(testPNG(testIHDR(1, 1, 8, 2), testChunk("IEND", nil)))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "no IDAT chunk") {
		t.Errorf("problems = %v", p.Problems)
	}
}

func TestIENDRules(t *testing.T) {
	// Missing IEND.
	p, err := Parse(testPNG(testIHDR(1, 1, 8, 2), testChunk("IDAT", []byte{0x00})))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "missing required IEND chunk") {
		t.Errorf("problems = %v", p.Problems)
	}

	// IEND not last.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
		testChunk("tEXt", []byte("after\x00x")),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "IEND must be the last chunk") {
		t.Errorf("problems = %v", p.Problems)
	}

	// IEND with a non-empty payload.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", []byte{0xde, 0xad, 0xbe, 0xef}),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "IEND chunk length must be 0") {
		t.Errorf("problems = %v", p.Problems)
	}

	// Duplicate IEND.
	p, err = Parse(testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
		testChunk("IEND", nil),
	))
	if err != nil {
		t.Fatal(err)
	}
	if !hasProblem(p, "single IEND chunk allowed, found 2") {
		t.Errorf("problems = %v", p.Problems)
	}
}

func TestAncillaryDuplicationAllowed(t *testing.T) {
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("tEXt", []byte("one\x00first")),
		testChunk("tEXt", []byte("two\x00second")),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) != 0 {
		t.Errorf("duplicate ancillary chunks must be tolerated: %v", p.Problems)
	}
	if len(p.TextInfo) != 2 {
		t.Errorf("TextInfo = %d entries, want 2", len(p.TextInfo))
	}
}

func TestValidAndCriticalChunks(t *testing.T) {
	p, err := Parse(testMinimalPNG())
	if err != nil {
		t.Fatal(err)
	}
	if errs := p.Valid(); len(errs) != 0 {
		t.Errorf("Valid() = %v", errs)
	}

	// Force a problem so Valid() surfaces it.
	bad, _ := Parse(testPNG(testIHDR(1, 1, 8, 2), testChunk("IEND", nil)))
	if errs := bad.Valid(); len(errs) != 1 {
		t.Errorf("Valid() = %v, want 1 error", errs)
	}

	crit := p.CriticalChunks()
	var names []string
	for _, c := range crit {
		names = append(names, c.TypeString())
	}
	if got := strings.Join(names, ","); got != "IHDR,IDAT,IEND" {
		t.Errorf("CriticalChunks = %s, want IHDR,IDAT,IEND", got)
	}
}

func TestDescribeOutput(t *testing.T) {
	pngb := testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("tEXt", []byte("Author\x00pngparse")),
		testChunk("IDAT", []byte{0x00}),
		testChunk("IEND", nil),
	)
	p, err := Parse(pngb)
	if err != nil {
		t.Fatal(err)
	}
	out := Describe(p)
	for _, want := range []string{"Signature: OK", "IHDR", "IDAT", "IEND", "width:", "Author: pngparse", "Validation: ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("Describe output missing %q:\n%s", want, out)
		}
	}

	// A bad CRC makes the chunk table flag BAD and sets Validation.
	bad := flipByte(testMinimalPNG(), 42)
	p, err = Parse(bad)
	if err != nil {
		t.Fatal(err)
	}
	out = Describe(p)
	if !strings.Contains(out, "BAD") {
		t.Errorf("Describe should flag BAD CRC:\n%s", out)
	}
	if !strings.Contains(out, "invalid CRC") {
		t.Errorf("Describe should include CRC problem:\n%s", out)
	}
}

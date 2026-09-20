package pngparse

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"
)

var testSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// testChunk builds one full chunk (length + type + data + CRC-32/ISO-HDLC).
func testChunk(t string, data []byte) []byte {
	typeData := make([]byte, 0, 4+len(data))
	typeData = append(typeData, t...)
	typeData = append(typeData, data...)

	out := make([]byte, 0, 8+len(data)+4)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], uint32(len(data)))
	out = append(out, u32[:]...)
	out = append(out, typeData...)
	binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(typeData))
	out = append(out, u32[:]...)
	return out
}

// testPNG assembles a PNG byte stream: signature followed by the given chunk
// byte sequences.
func testPNG(parts ...[]byte) []byte {
	out := append([]byte(nil), testSignature...)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func testIHDR(w, h uint32, bit, color byte) []byte {
	return testIHDRFull(w, h, bit, color, 0, 0, 0)
}

func testIHDRFull(w, h uint32, bit, color, comp, filt, interl byte) []byte {
	d := make([]byte, 13)
	binary.BigEndian.PutUint32(d[0:4], w)
	binary.BigEndian.PutUint32(d[4:8], h)
	d[8], d[9], d[10], d[11], d[12] = bit, color, comp, filt, interl
	return testChunk("IHDR", d)
}

// testMinimalPNG returns a structurally healthy 1x1 RGB PNG.
func testMinimalPNG() []byte {
	return testPNG(
		testIHDR(1, 1, 8, 2),
		testChunk("IDAT", []byte{0x09, 0x00, 0x00, 0xff}),
		testChunk("IEND", nil),
	)
}

// testCompress zlib-compresses data.
func testCompress(b []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		panic(err)
	}
	if err := zw.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// flipByte returns a copy of b with the byte at idx XORed.
func flipByte(b []byte, idx int) []byte {
	c := append([]byte(nil), b...)
	c[idx] ^= 0x01
	return c
}

// hasProblem reports whether any problem contains substr.
func hasProblem(p *PNG, substr string) bool {
	for _, pr := range p.Problems {
		if strings.Contains(pr, substr) {
			return true
		}
	}
	return false
}

// hasWarning reports whether any warning contains substr.
func hasWarning(p *PNG, substr string) bool {
	for _, w := range p.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func noProblems(t *testing.T, p *PNG) {
	t.Helper()
	if len(p.Problems) > 0 {
		t.Fatalf("expected no problems, got:\n  %s\n", strings.Join(p.Problems, "\n  "))
	}
}

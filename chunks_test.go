package pngparse

import (
	"encoding/binary"
	"hash/crc32"
	"strings"
	"testing"
)

// TestHandComputedChunk feeds a chunk whose CRC was computed independently
// (by hand, and pinned to a constant) and verifies ParseChunks agrees.
// CRC-32/ISO-HDLC of the bytes "TESTabcd" is 0x293279d4.
func TestHandComputedChunk(t *testing.T) {
	sig := testSignature
	raw := append([]byte(nil), sig...)
	raw = append(raw, 0x00, 0x00, 0x00, 0x04) // length 4
	raw = append(raw, 'T', 'E', 'S', 'T')     // type
	raw = append(raw, 'a', 'b', 'c', 'd')     // data
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], 0x293279d4)
	raw = append(raw, crc[:]...)

	chunks, err := ParseChunks(raw)
	if err != nil {
		t.Fatalf("ParseChunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	c := chunks[0]
	if c.TypeString() != "TEST" {
		t.Errorf("type = %q, want TEST", c.TypeString())
	}
	if c.Length != 4 {
		t.Errorf("length = %d, want 4", c.Length)
	}
	if c.CrcValue != 0x293279d4 {
		t.Errorf("crc = %08x, want 293279d4", c.CrcValue)
	}
	if !c.CrcValid {
		t.Error("hand-computed CRC should validate")
	}
	if c.Offset != 8 {
		t.Errorf("offset = %d, want 8", c.Offset)
	}
	if string(c.Data) != "abcd" {
		t.Errorf("data = %q, want abcd", c.Data)
	}
}

func TestParseChunksMinimalRGB(t *testing.T) {
	pngb := testMinimalPNG()
	chunks, err := ParseChunks(pngb)
	if err != nil {
		t.Fatalf("ParseChunks: %v", err)
	}
	want := []struct {
		typ    string
		length uint32
		off    int64
	}{
		{"IHDR", 13, 8},
		{"IDAT", 4, 33},
		{"IEND", 0, 49},
	}
	if len(chunks) != len(want) {
		t.Fatalf("got %d chunks, want %d", len(chunks), len(want))
	}
	// File size: signature 8 + (25 + 16 + 12) = 61.
	if len(pngb) != 61 {
		t.Errorf("file length = %d, want 61", len(pngb))
	}
	for i, w := range want {
		c := chunks[i]
		if c.TypeString() != w.typ {
			t.Errorf("chunk %d type = %q, want %q", i, c.TypeString(), w.typ)
		}
		if c.Length != w.length {
			t.Errorf("chunk %d length = %d, want %d", i, c.Length, w.length)
		}
		if c.Offset != w.off {
			t.Errorf("chunk %d offset = %d, want %d", i, c.Offset, w.off)
		}
		if !c.CrcValid {
			t.Errorf("chunk %d (%s) CRC should be valid", i, w.typ)
		}
		// Independent CRC re-computation over Type+Data.
		body := append([]byte(c.TypeString()), c.Data...)
		if got := crc32.ChecksumIEEE(body); got != c.CrcValue {
			t.Errorf("chunk %d CRC mismatch: computed %08x, stored %08x", i, got, c.CrcValue)
		}
	}
}

func TestParseChunksTruncationOffset(t *testing.T) {
	pngb := testMinimalPNG()
	// Cut just before the IDAT chunk's CRC: 12 bytes present from offset 33
	// (length+type+full 4-byte data) but the 4 CRC bytes are missing.
	cut := pngb[:45]
	chunks, err := ParseChunks(cut)
	if err == nil {
		t.Fatal("expected truncation error")
	}
	if !strings.Contains(err.Error(), "truncated") {
		t.Errorf("error = %v, want truncation message", err)
	}
	if !strings.Contains(err.Error(), "offset 33") {
		t.Errorf("error = %v, want offset 33", err)
	}
	if !strings.Contains(err.Error(), "IDAT") {
		t.Errorf("error = %v, want chunk name IDAT", err)
	}
	// The intact preamble (IHDR) must still be returned.
	if len(chunks) != 1 || chunks[0].TypeString() != "IHDR" {
		t.Fatalf("got chunks %v, want the IHDR only", chunks)
	}

	// Cutting inside the chunk header itself reports the header offset.
	chunks, err = ParseChunks(pngb[:15])
	if err == nil {
		t.Fatal("expected truncation error for header")
	}
	if !strings.Contains(err.Error(), "offset 8") {
		t.Errorf("error = %v, want offset 8", err)
	}
	if len(chunks) != 0 {
		t.Errorf("no full chunk should be returned, got %d", len(chunks))
	}
}

func TestParseChunksCorruptTypeByte(t *testing.T) {
	pngb := testMinimalPNG()
	// The IHDR type bytes live at offsets 12..15; corrupt the second byte.
	bad := flipByte(pngb, 13)
	bad[13] = 0x00
	_, err := ParseChunks(bad)
	if err == nil {
		t.Fatal("expected corrupt chunk type error")
	}
	if !strings.Contains(err.Error(), "corrupt chunk type") || !strings.Contains(err.Error(), "offset 8") {
		t.Errorf("error = %v, want corrupt-type message with offset 8", err)
	}
}

func TestBadCRCRecordedNotFatal(t *testing.T) {
	pngb := testMinimalPNG()
	// Flip a byte inside the IDAT payload (offset 41..44).
	bad := flipByte(pngb, 42)

	chunks, err := ParseChunks(bad)
	if err != nil {
		t.Fatalf("ParseChunks should not fail on a bad CRC: %v", err)
	}
	found := false
	for _, c := range chunks {
		if c.TypeString() == "IDAT" {
			found = true
			if c.CrcValid {
				t.Error("IDAT CRC should be flagged invalid")
			}
		} else if !c.CrcValid {
			t.Errorf("%s CRC flagged invalid unexpectedly", c.TypeString())
		}
	}
	if !found {
		t.Fatal("IDAT chunk missing from parse")
	}

	r := Validate(bad)
	if !r.SignatureOK {
		t.Error("Validate should report signature OK")
	}
	if len(r.Problems) == 0 {
		t.Fatal("Validate should report a CRC problem")
	}
	if !strings.Contains(strings.Join(r.Problems, "; "), "invalid CRC") {
		t.Errorf("Validate problems = %v, want invalid CRC", r.Problems)
	}
}

func TestValidateNotPNG(t *testing.T) {
	r := Validate([]byte("definitely not an image"))
	if r.SignatureOK {
		t.Error("Validate should report SignatureOK=false")
	}
	if len(r.Problems) == 0 {
		t.Error("Validate should report the bad-signature problem")
	}
	r2 := Validate(testMinimalPNG())
	if !r2.SignatureOK || len(r2.Problems) != 0 {
		t.Errorf("healthy file: SignatureOK=%v Problems=%v", r2.SignatureOK, r2.Problems)
	}
}

func TestFindAndLookupChunks(t *testing.T) {
	chunks, _ := ParseChunks(testMinimalPNG())
	if got := len(FindChunks(chunks, "IDAT")); got != 1 {
		t.Errorf("FindChunks(IDAT) = %d, want 1", got)
	}
	if got := len(FindChunks(chunks, "tEXt")); got != 0 {
		t.Errorf("FindChunks(tEXt) = %d, want 0", got)
	}
	c, ok := LookupByType(chunks, "IHDR")
	if !ok || c.TypeString() != "IHDR" {
		t.Errorf("LookupByType(IHDR) = (%v, %v), want ok", c, ok)
	}
	if _, ok := LookupByType(chunks, "none"); ok {
		t.Error("LookupByType should miss for unknown type")
	}
}

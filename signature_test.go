package pngparse

import (
	"errors"
	"testing"
)

func TestValidSignature(t *testing.T) {
	// First eight bytes of every real PNG, hardcoded.
	real := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !ValidSignature(real) {
		t.Error("the real PNG signature was rejected")
	}
	if !ValidSignature(testSignature) {
		t.Error("test signature was rejected")
	}
	// A signature followed by more data is still valid.
	long := append([]byte(nil), real...)
	long = append(long, 0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R')
	if !ValidSignature(long) {
		t.Error("signature plus header should validate")
	}

	invalid := [][]byte{
		nil,
		{},
		{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a}, // 7 bytes
		{0x89, 'P', 'N', 'G'},
		[]byte("PNG\r\n\x1a\n"),
		[]byte("GIF89a\x01\x02\x03"),
		[]byte("not a png"),
	}
	for _, b := range invalid {
		if ValidSignature(b) {
			t.Errorf("ValidSignature(%q) = true, want false", b)
		}
	}
}

func TestErrNotPNG(t *testing.T) {
	for _, b := range [][]byte{
		nil,
		[]byte("hello world"),
		[]byte("GIF89a\x01\x02\x03\x04"),
		[]byte{0x89, 'P', 'N', 'G'}, // truncated signature
	} {
		if _, err := ParseChunks(b); !errors.Is(err, ErrNotPNG) {
			t.Errorf("ParseChunks(%q) err = %v, want ErrNotPNG", b, err)
		}
		if _, err := Parse(b); !errors.Is(err, ErrNotPNG) {
			t.Errorf("Parse(%q) err = %v, want ErrNotPNG", b, err)
		}
	}
}

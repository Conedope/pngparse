package pngparse

import "errors"

// PNGSignature is the fixed 8-byte file signature defined by ISO/IEC 15948
// (and the W3C PNG specification): 0x89 'P' 'N' 'G' CR LF 0x1A LF.
var PNGSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// ErrNotPNG is returned when input does not begin with the PNG signature.
var ErrNotPNG = errors.New("pngparse: not a PNG file (bad signature)")

// ValidSignature reports whether b begins with the 8-byte PNG signature.
// A slice shorter than the signature is never valid.
func ValidSignature(b []byte) bool {
	if len(b) < len(PNGSignature) {
		return false
	}
	for i, c := range PNGSignature {
		if b[i] != c {
			return false
		}
	}
	return true
}

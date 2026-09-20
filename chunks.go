package pngparse

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"strings"
)

// Chunk is a single raw PNG chunk as found in the file, including the
// signature that precedes its payload. Offset is the byte offset at which
// the chunk's length field starts (the first byte after the previous chunk).
type Chunk struct {
	Type     [4]byte
	Length   uint32
	Data     []byte
	CrcValue uint32
	CrcValid bool // computed CRC-32/ISO-HDLC over Type+Data == stored CRC
	Offset   int64
}

// TypeString returns the four type bytes as a string.
func (c Chunk) TypeString() string { return string(c.Type[:]) }

// Report is the graceful, non-fatal result of Validate: it records every
// inspectable chunk plus a list of problems such as truncation, corrupt
// chunk types and CRC mismatches.
type Report struct {
	SignatureOK bool
	Chunks      []Chunk
	Problems    []string
}

// isChunkTypeByte reports whether a chunk-type byte is allowed by the PNG
// specification: an uppercase letter, a lowercase letter, or the space
// character (space is tolerated by some lossy decoders when repairing
// files, and is accepted here as an ancillary flag placeholder).
func isChunkTypeByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == ' '
}

// parseChunks walks the byte stream without failing on CRC mismatches.
// It returns every chunk read so far and a list of structural problems,
// stopping at the first truncation or corrupt-type error. The signature is
// assumed to have been validated by the caller.
func parseChunks(data []byte) ([]Chunk, []string) {
	var chunks []Chunk
	var problems []string
	total := int64(len(data))
	off := int64(len(PNGSignature))

	for off < total {
		// Minimum remaining: length(4) + type(4) + crc(4), data >= 0.
		if total-off < 12 {
			problems = append(problems, fmt.Sprintf("truncated chunk header at offset %d", off))
			break
		}
		length := binary.BigEndian.Uint32(data[off : off+4])
		typ := [4]byte{}
		copy(typ[:], data[off+4:off+8])

		end := off + 8 + int64(length) + 4
		if end > total {
			problems = append(problems, fmt.Sprintf(
				"truncated %q chunk at offset %d: need %d bytes but only %d remain",
				string(typ[:]), off, 8+int64(length)+4, total-off))
			break
		}

		body := make([]byte, 0, 4+int(length))
		body = append(body, typ[:]...)
		body = append(body, data[off+8:off+8+int64(length)]...)

		bad := false
		for _, b := range typ {
			if !isChunkTypeByte(b) {
				bad = true
				break
			}
		}
		if bad {
			problems = append(problems, fmt.Sprintf(
				"corrupt chunk type %q at offset %d (type bytes must be A-Z, a-z or space)",
				string(typ[:]), off))
			break
		}

		dataCopy := make([]byte, length)
		copy(dataCopy, data[off+8:off+8+int64(length)])

		stored := binary.BigEndian.Uint32(data[off+8+int64(length) : end])
		computed := crc32.ChecksumIEEE(body)

		chunks = append(chunks, Chunk{
			Type:     typ,
			Length:   length,
			Data:     dataCopy,
			CrcValue: stored,
			CrcValid: computed == stored,
			Offset:   off,
		})
		off = end
	}
	return chunks, problems
}

// ParseChunks validates the signature and walks every chunk in order,
// verifying each CRC-32 (recorded on the chunk, never fatal) and failing on
// structural errors: a missing/invalid signature (ErrNotPNG), a chunk whose
// body overruns the buffer, or a chunk type containing bytes outside the
// allowed alphabet. On a truncation error the chunks read so far are still
// returned alongside the error.
func ParseChunks(data []byte) ([]Chunk, error) {
	if !ValidSignature(data) {
		return nil, ErrNotPNG
	}
	chunks, problems := parseChunks(data)
	if len(problems) > 0 {
		return chunks, errors.New(strings.Join(problems, "; "))
	}
	return chunks, nil
}

// Validate is the graceful inspect-only entry point. It never fails: a file
// without the PNG signature yields a Report with SignatureOK false; every
// other anomaly — truncation, corrupt chunk types, CRC mismatches — is
// collected into Report.Problems while the chunks remain inspectable.
func Validate(data []byte) *Report {
	r := &Report{}
	if !ValidSignature(data) {
		r.Problems = append(r.Problems, ErrNotPNG.Error())
		return r
	}
	r.SignatureOK = true
	chunks, problems := parseChunks(data)
	r.Chunks = chunks
	r.Problems = append(r.Problems, problems...)
	for _, c := range chunks {
		if !c.CrcValid {
			body := make([]byte, 0, 4+len(c.Data))
			body = append(body, c.Type[:]...)
			body = append(body, c.Data...)
			r.Problems = append(r.Problems, fmt.Sprintf(
				"chunk %q at offset %d has invalid CRC (stored %08x, computed %08x)",
				c.TypeString(), c.Offset, c.CrcValue, crc32.ChecksumIEEE(body)))
		}
	}
	return r
}

// FindChunks returns every chunk whose type equals typeStr, in file order.
func FindChunks(chunks []Chunk, typeStr string) []Chunk {
	var out []Chunk
	for _, c := range chunks {
		if c.TypeString() == typeStr {
			out = append(out, c)
		}
	}
	return out
}

// LookupByType returns the first chunk of the given type, if any.
func LookupByType(chunks []Chunk, typeStr string) (Chunk, bool) {
	for _, c := range chunks {
		if c.TypeString() == typeStr {
			return c, true
		}
	}
	return Chunk{}, false
}

// IsCritical reports whether a chunk type begins with an uppercase letter,
// which the PNG specification defines as marking a critical chunk.
func (c Chunk) IsCritical() bool {
	return c.Type[0] >= 'A' && c.Type[0] <= 'Z'
}

// Command pngparse is a PNG structural inspector and validator. It parses
// the signature, walks every chunk, verifies all CRCs, decodes the IHDR and
// text chunks, and flags structural violations.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Conedope/pngparse"
)

const version = "0.1.0"

func usage(w io.Writer) {
	fmt.Fprintf(w, `pngparse %s — PNG structural inspector and validator

Usage:
  pngparse [flags] FILE

Flags:
  --json            emit a machine-readable JSON report
  --extract TYPE    print text entries of the given chunk type (tEXt, zTXt, iTXt)
  --hash            print the SHA-256 hex digest of the raw file bytes
  --version         print the version and exit
  --help            show this help

FILE must begin with the 8-byte PNG signature. Exit code is 1 when the file
has structural problems, a bad CRC, or is not a PNG.
`, version)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type jsonHeader struct {
	Width       uint32 `json:"width"`
	Height      uint32 `json:"height"`
	BitDepth    byte   `json:"bit_depth"`
	ColorType   byte   `json:"color_type"`
	Compression byte   `json:"compression"`
	Filter      byte   `json:"filter"`
	Interlace   byte   `json:"interlace"`
}

type jsonChunk struct {
	Offset   int64  `json:"offset"`
	Type     string `json:"type"`
	Length   uint32 `json:"length"`
	Critical bool   `json:"critical"`
	CrcValid bool   `json:"crc_valid"`
	CrcValue uint32 `json:"crc_value"`
}

type jsonReport struct {
	SignatureOK bool                 `json:"signature_ok"`
	Header      jsonHeader           `json:"header"`
	Chunks      []jsonChunk          `json:"chunks"`
	Text        []pngparse.TextEntry `json:"text"`
	Critical    []string             `json:"critical_chunks"`
	Problems    []string             `json:"problems"`
	Warnings    []string             `json:"warnings"`
	SHA256      string               `json:"sha256,omitempty"`
}

func buildJSON(p *pngparse.PNG, sum string) jsonReport {
	r := jsonReport{
		SignatureOK: true,
		Header: jsonHeader{
			Width:       p.Header.Width,
			Height:      p.Header.Height,
			BitDepth:    p.Header.BitDepth,
			ColorType:   p.Header.ColorType,
			Compression: p.Header.Compression,
			Filter:      p.Header.Filter,
			Interlace:   p.Header.Interlace,
		},
		Text:     p.TextInfo,
		Problems: p.Problems,
		Warnings: p.Warnings,
		SHA256:   sum,
	}
	if r.Problems == nil {
		r.Problems = []string{}
	}
	if r.Warnings == nil {
		r.Warnings = []string{}
	}
	for _, c := range p.Chunks {
		r.Chunks = append(r.Chunks, jsonChunk{
			Offset:   c.Offset,
			Type:     c.TypeString(),
			Length:   c.Length,
			Critical: c.IsCritical(),
			CrcValid: c.CrcValid,
			CrcValue: c.CrcValue,
		})
	}
	for _, c := range p.CriticalChunks() {
		r.Critical = append(r.Critical, c.TypeString())
	}
	return r
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pngparse", flag.ContinueOnError)
	fs.SetOutput(stderr)
	doJSON := fs.Bool("json", false, "emit a JSON report")
	doExtract := fs.String("extract", "", "print text entries of a chunk type")
	doHash := fs.Bool("hash", false, "print the SHA-256 hex digest")
	doVersion := fs.Bool("version", false, "print the version")
	doHelp := fs.Bool("help", false, "show help")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *doVersion {
		fmt.Fprintf(stdout, "pngparse %s\n", version)
		return 0
	}
	if *doHelp {
		usage(stdout)
		return 0
	}
	if fs.NArg() == 0 {
		usage(stderr)
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "pngparse: exactly one FILE argument is expected")
		return 2
	}

	path := fs.Arg(0)
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "pngparse: %v\n", err)
		return 1
	}

	sum := ""
	if *doHash || *doJSON {
		h := sha256.Sum256(data)
		sum = hex.EncodeToString(h[:])
		if !*doJSON {
			fmt.Fprintf(stdout, "sha256 %s  %s\n", sum, path)
		}
	}

	p, err := pngparse.Parse(data)
	if err != nil {
		if errors.Is(err, pngparse.ErrNotPNG) {
			fmt.Fprintln(stderr, "pngparse: not a PNG file")
		} else {
			fmt.Fprintf(stderr, "pngparse: %v\n", err)
		}
		return 1
	}

	switch {
	case *doJSON:
		out, err := json.MarshalIndent(buildJSON(p, sum), "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "pngparse: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(out))
	case *doExtract != "":
		n := 0
		for _, t := range p.TextInfo {
			if t.Kind == *doExtract {
				fmt.Fprintf(stdout, "%s: %s\n", t.Keyword, t.Text)
				n++
			}
		}
		if n == 0 {
			fmt.Fprintf(stderr, "pngparse: no %s text entries found\n", *doExtract)
		}
	default:
		fmt.Fprint(stdout, pngparse.Describe(p))
	}

	if len(p.Problems) > 0 {
		return 1
	}
	return 0
}

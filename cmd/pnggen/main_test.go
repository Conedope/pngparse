package main

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Conedope/pngparse"
)

func TestGenerateWritesValidFixtures(t *testing.T) {
	dir := t.TempDir()
	if err := write(dir); err != nil {
		t.Fatal(err)
	}

	indexed := filepath.Join(dir, "1x1-indexed.png")
	rgb := filepath.Join(dir, "2x2-rgb-text.png")
	corrupt := filepath.Join(dir, "2x2-rgb-text-corrupt.png")

	for _, p := range []string{indexed, rgb, corrupt} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("generator did not produce %s", p)
		}
	}

	// Both healthy fixtures must decode with image/png.
	for _, p := range []string{indexed, rgb} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%s failed to decode: %v", p, err)
			continue
		}
		_ = im
	}

	// The corrupt copy must be structurally parseable but carry a bad CRC.
	data, err := os.ReadFile(corrupt)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pngparse.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	gotCRCProblem := false
	for _, pr := range p.Problems {
		if strings.Contains(pr, "invalid CRC") {
			gotCRCProblem = true
		}
	}
	if !gotCRCProblem {
		t.Errorf("corrupt fixture problems = %v, want invalid CRC", p.Problems)
	}
	healthy, err := os.ReadFile(rgb)
	if err != nil {
		t.Fatal(err)
	}
	if string(healthy) == string(data) {
		t.Error("corrupt fixture must differ from the healthy one")
	}
}

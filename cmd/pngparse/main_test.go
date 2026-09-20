package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pngparse-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "pngparse")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "building pngparse failed: %v\n%s\n", err, out)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// localPNG builds a minimal 2x2 RGB PNG with tEXt, useful for CLI tests.
func localPNG() []byte {
	sig := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	chunk := func(t string, data []byte) []byte {
		typeData := append([]byte(t), data...)
		var u32 [4]byte
		binary.BigEndian.PutUint32(u32[:], uint32(len(data)))
		var out []byte
		out = append(out, u32[:]...)
		out = append(out, typeData...)
		binary.BigEndian.PutUint32(u32[:], crc32.ChecksumIEEE(typeData))
		out = append(out, u32[:]...)
		return out
	}
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 2)
	binary.BigEndian.PutUint32(ihdr[4:], 2)
	ihdr[8], ihdr[9] = 8, 2

	var out []byte
	out = append(out, sig...)
	out = append(out, chunk("IHDR", ihdr)...)
	out = append(out, chunk("tEXt", []byte("Author\x00pngparse CLI test"))...)
	out = append(out, chunk("IDAT", []byte{0x00, 0xff, 0x00, 0x00})...)
	out = append(out, chunk("IEND", nil)...)
	return out
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func runBin(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("running binary: %v", err)
		}
	}
	return so.String(), se.String(), code
}

func TestCLICleanFileExitZero(t *testing.T) {
	path := writeTemp(t, "clean.png", localPNG())
	stdout, stderr, code := runBin(t, path)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{"Signature: OK", "IHDR", "tEXt", "Author: pngparse CLI test", "Validation: ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

func TestCLICorruptedCRCExitOne(t *testing.T) {
	good := localPNG()
	idxt := bytes.Index(good, []byte("IDAT"))
	bad := localPNG()
	payload := idxt + 4 + 2 // middle of IDAT data
	bad[payload] ^= 0xFF

	path := writeTemp(t, "corrupt.png", bad)
	stdout, _, code := runBin(t, path)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout, "invalid CRC") {
		t.Errorf("stdout should report invalid CRC:\n%s", stdout)
	}
	if !strings.Contains(stdout, "BAD") {
		t.Errorf("stdout should flag BAD CRC:\n%s", stdout)
	}
}

func TestCLINotPNGExitOne(t *testing.T) {
	path := writeTemp(t, "junk.bin", []byte("definitely not a png file, sorry"))
	_, stderr, code := runBin(t, path)
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "not a PNG") {
		t.Errorf("stderr = %q, want not-a-PNG message", stderr)
	}
}

func TestCLIMissingFileExitOne(t *testing.T) {
	_, stderr, code := runBin(t, filepath.Join(t.TempDir(), "nope.png"))
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "pngparse:") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestCLIJSONParsable(t *testing.T) {
	path := writeTemp(t, "clean.png", localPNG())
	stdout, _, code := runBin(t, "--json", path)
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	var rep map[string]any
	if err := json.Unmarshal([]byte(stdout), &rep); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, stdout)
	}
	if rep["signature_ok"] != true {
		t.Errorf("signature_ok = %v", rep["signature_ok"])
	}
	chunks, ok := rep["chunks"].([]any)
	if !ok || len(chunks) != 4 {
		t.Errorf("chunks = %v, want 4", rep["chunks"])
	}
	header, ok := rep["header"].(map[string]any)
	if !ok || header["width"] != float64(2) || header["height"] != float64(2) {
		t.Errorf("header = %v", rep["header"])
	}
	if probs, ok := rep["problems"].([]any); !ok || len(probs) != 0 {
		t.Errorf("problems = %v", rep["problems"])
	}
	if sum, _ := rep["sha256"].(string); len(sum) != 64 {
		t.Errorf("sha256 = %q", sum)
	}
}

func TestCLIExtract(t *testing.T) {
	path := writeTemp(t, "clean.png", localPNG())
	stdout, _, code := runBin(t, "--extract", "tEXt", path)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.Contains(stdout, "Author: pngparse CLI test") {
		t.Errorf("stdout = %q", stdout)
	}
}

func TestCLIHash(t *testing.T) {
	data := localPNG()
	path := writeTemp(t, "clean.png", data)
	sum := sha256.Sum256(data)
	stdout, _, code := runBin(t, "--hash", path)
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	wantHash := hex.EncodeToString(sum[:])
	if !strings.Contains(stdout, wantHash) {
		t.Errorf("stdout missing sha256 %s:\n%s", wantHash, stdout)
	}
}

func TestCLIVersionAndHelp(t *testing.T) {
	stdout, _, code := runBin(t, "--version")
	if code != 0 || !strings.Contains(stdout, "pngparse 0.1.0") {
		t.Errorf("--version: code=%d stdout=%q", code, stdout)
	}
	stdout, _, code = runBin(t, "--help")
	if code != 0 || !strings.Contains(stdout, "Usage:") {
		t.Errorf("--help: code=%d stdout=%q", code, stdout)
	}
}

func TestCLIUsageNoArgs(t *testing.T) {
	_, stderr, code := runBin(t)
	if code != 2 {
		t.Errorf("no-args exit = %d, want 2", code)
	}
	if !strings.Contains(stderr, "Usage:") {
		t.Errorf("stderr = %q", stderr)
	}
}

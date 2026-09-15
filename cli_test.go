package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Carts committed under testdata/. See testdata/README.md for where each one
// came from.
const (
	celesteCart = "testdata/celeste.p8.png"
	testCart    = "testdata/test_cart.p8.png"
	golCart     = "testdata/test_gol.p8.png"
	emptyCart   = "testdata/empty.p8.png"
)

// runCLI invokes the command the way main does, and returns its exit code
// alongside everything it wrote.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(append([]string{"pico8-data-extractor"}, args...), &out, &errOut)
	return code, out.String(), errOut.String()
}

// readFile reads a file the test expects to exist.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(data)
}

// p8Sections splits .p8 text into its named sections. Trailing blank lines are
// dropped: a cart round-trip loses trailing whitespace from the source, and
// PICO-8 itself is inconsistent about the blank line before __gff__.
func p8Sections(text string) map[string][]string {
	sections := make(map[string][]string)
	name := "header"
	var lines []string

	flush := func() {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		sections[name] = lines
		lines = nil
	}

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "__") && strings.HasSuffix(line, "__") {
			flush()
			name = line
			continue
		}
		lines = append(lines, line)
	}
	flush()
	return sections
}

// assertSameSections compares converted .p8 text against a reference cart,
// section by section.
func assertSameSections(t *testing.T, got, want string) {
	t.Helper()
	gotSections, wantSections := p8Sections(got), p8Sections(want)

	if len(gotSections) != len(wantSections) {
		t.Fatalf("section count = %d, want %d", len(gotSections), len(wantSections))
	}
	for name, wantLines := range wantSections {
		gotLines, ok := gotSections[name]
		if !ok {
			t.Errorf("missing section %s", name)
			continue
		}
		if len(gotLines) != len(wantLines) {
			t.Errorf("%s: %d lines, want %d", name, len(gotLines), len(wantLines))
			continue
		}
		for i := range wantLines {
			if gotLines[i] != wantLines[i] {
				t.Errorf("%s line %d:\n got %q\nwant %q", name, i+1, gotLines[i], wantLines[i])
				break
			}
		}
	}
}

// Every vendored cart must convert to the same sections an independent
// implementation recorded for it.
func TestRunConvertsRealCarts(t *testing.T) {
	tests := []struct {
		name      string
		cart      string
		reference string
	}{
		{"test cart", testCart, "testdata/test_cart.p8"},
		{"game of life", golCart, "testdata/test_gol.p8"},
		{"empty cart", emptyCart, "testdata/empty.p8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "out.p8")
			code, stdout, stderr := runCLI(t, tt.cart, out)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			if !strings.Contains(stdout, "Cart converted and saved to") {
				t.Errorf("stdout = %q", stdout)
			}
			assertSameSections(t, readFile(t, out), readFile(t, tt.reference))
		})
	}
}

// test_cart's reference was written by PICO-8 without the trailing-whitespace
// loss the others show, so it pins the output byte for byte.
func TestRunConvertsTestCartByteForByte(t *testing.T) {
	out := filepath.Join(t.TempDir(), "out.p8")
	if code, _, stderr := runCLI(t, testCart, out); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if got, want := readFile(t, out), readFile(t, "testdata/test_cart.p8"); got != want {
		t.Error("converted cart does not match the reference byte for byte")
	}
}

// Celeste has no reference cart, so its conversion is pinned by checksum and
// by the shape every .p8 must have.
func TestRunConvertsCeleste(t *testing.T) {
	out := filepath.Join(t.TempDir(), "celeste.p8")
	if code, _, stderr := runCLI(t, celesteCart, out); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}

	got := readFile(t, out)
	sum := sha256.Sum256([]byte(got))
	want := strings.TrimSpace(readFile(t, "testdata/celeste.p8.sha256"))
	if hex.EncodeToString(sum[:]) != want {
		t.Errorf("checksum = %s, want %s", hex.EncodeToString(sum[:]), want)
	}

	sections := p8Sections(got)
	if lines := sections["header"]; len(lines) != 2 || lines[1] != "version 5" {
		t.Errorf("header = %v", lines)
	}
	if lines := sections["__lua__"]; len(lines) == 0 || lines[0] != "-- ~celeste~" {
		t.Errorf("lua section does not start with the cart title")
	}
	for _, tt := range []struct {
		section string
		lines   int
	}{
		{"__gfx__", 128},
		{"__gff__", 2},
		{"__map__", 32},
		{"__sfx__", 64},
		{"__music__", 64},
	} {
		if got := len(sections[tt.section]); got != tt.lines {
			t.Errorf("%s: %d lines, want %d", tt.section, got, tt.lines)
		}
	}
}

func TestRunPrintsLuaToStdout(t *testing.T) {
	code, stdout, stderr := runCLI(t, celesteCart)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "-- ~celeste~\n-- matt thorson + noel berry\n") {
		t.Errorf("stdout starts with %q", stdout[:40])
	}
}

func TestRunWritesLuaToFile(t *testing.T) {
	out := filepath.Join(t.TempDir(), "celeste.lua")
	code, stdout, stderr := runCLI(t, celesteCart, out)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Lua code extracted and saved to") {
		t.Errorf("stdout = %q", stdout)
	}
	if !strings.HasPrefix(readFile(t, out), "-- ~celeste~") {
		t.Error("lua file does not start with the cart title")
	}
}

// The .p8 suffix is matched case-insensitively.
func TestRunUppercaseP8Suffix(t *testing.T) {
	out := filepath.Join(t.TempDir(), "OUT.P8")
	if code, _, stderr := runCLI(t, testCart, out); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.HasPrefix(readFile(t, out), "pico-8 cartridge") {
		t.Error("uppercase .P8 output is not a cart")
	}
}

func TestRunExtractsDirectory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cart") + string(os.PathSeparator)
	code, stdout, stderr := runCLI(t, testCart, out)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "Extracted cart data into") {
		t.Errorf("stdout = %q", stdout)
	}

	for _, name := range []string{
		"test_cart.p8",
		"metadata.json",
		"spritesheet.png",
		"spritesheet.json",
		"map.png",
		"map.json",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("missing %s: %v", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(out, "sprites"))
	if err != nil {
		t.Fatalf("reading sprites: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no sprites written")
	}
}

// --only=p8 keeps the converted cart and never calls the extraction library.
func TestRunOnlyP8(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cart") + string(os.PathSeparator)
	if code, _, stderr := runCLI(t, testCart, out, "--only=p8"); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatalf("reading output dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "test_cart.p8" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("output = %v, want just the .p8", names)
	}
}

// A library-only filter drops the intermediate .p8 once it has been consumed.
func TestRunOnlyLibraryCategory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cart") + string(os.PathSeparator)
	if code, _, stderr := runCLI(t, testCart, out, "--only=metadata"); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}

	if _, err := os.Stat(filepath.Join(out, "metadata.json")); err != nil {
		t.Errorf("missing metadata.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "test_cart.p8")); !os.IsNotExist(err) {
		t.Error("intermediate .p8 was kept")
	}
	if _, err := os.Stat(filepath.Join(out, "spritesheet.png")); !os.IsNotExist(err) {
		t.Error("unselected category was written")
	}
}

// An unknown category warns but does not fail.
func TestRunOnlyUnknownCategory(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cart") + string(os.PathSeparator)
	code, _, stderr := runCLI(t, testCart, out, "--only=bogus")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if !strings.Contains(stderr, `unknown --only category "bogus"`) {
		t.Errorf("stderr = %q", stderr)
	}
}

// Generated carts cover the code formats the vendored carts do not reach: the
// picotool carts are version 8 and Celeste is version 5, so none of them are
// pxa compressed.
func TestRunGeneratedCarts(t *testing.T) {
	const source = "-- generated\nfunction _init()\n x=1\nend\n"

	tests := []struct {
		name    string
		code    []byte
		version byte
		wantLua string
	}{
		// The uncompressed reader terminates the region with a newline of its
		// own, the way picotool does, so it gains one over the source.
		{"uncompressed", []byte(source), 0, source + "\n"},
		{"old compression", compressOld([]byte(source)), 8, source},
		{"pxa compression", compressPXA([]byte(source)), 41, source},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cart := writeCartPNG(t, buildROM(tt.code, tt.version))

			code, stdout, stderr := runCLI(t, cart)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			if stdout != tt.wantLua {
				t.Errorf("lua = %q, want %q", stdout, tt.wantLua)
			}

			out := filepath.Join(t.TempDir(), "out.p8")
			if code, _, stderr := runCLI(t, cart, out); code != 0 {
				t.Fatalf("converting: exit %d, stderr: %s", code, stderr)
			}
			sections := p8Sections(readFile(t, out))
			if got := strings.Join(sections["__lua__"], "\n"); got != strings.TrimRight(tt.wantLua, "\n") {
				t.Errorf("lua section = %q", got)
			}
			if got := len(sections["__gfx__"]); got != 128 {
				t.Errorf("__gfx__: %d lines, want 128", got)
			}
		})
	}
}

// writeFile writes content to a file in a temp directory and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// tinyPNG writes a valid PNG far too small to hold a cart ROM.
func tinyPNG(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tiny.p8.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating tiny png: %v", err)
	}
	defer f.Close() //nolint:errcheck
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encoding tiny png: %v", err)
	}
	return path
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no arguments", nil, "Usage: pico8-data-extractor"},
		{"missing file", []string{"testdata/nope.p8.png"}, "Error opening file"},
		{"not a png", []string{writeFile(t, "fake.p8.png", "not a png")}, "Error decoding PNG"},
		{"image too small", []string{tinyPNG(t)}, "cartridge data too small"},
		{"unwritable output", []string{testCart, filepath.Join(writeFile(t, "blocker", "x"), "out.p8")}, "Error writing to file"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, tt.args...)
			if code != 1 {
				t.Errorf("exit = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.want)
			}
		})
	}
}

// A directory output that cannot be created is reported, not ignored.
func TestRunDirectoryOutputError(t *testing.T) {
	blocker := writeFile(t, "blocker", "x")
	code, _, stderr := runCLI(t, testCart, filepath.Join(blocker, "out")+string(os.PathSeparator))
	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stderr, "Error extracting cart data") {
		t.Errorf("stderr = %q", stderr)
	}
}

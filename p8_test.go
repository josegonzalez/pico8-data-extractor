package main

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"strings"
	"testing"
)

func TestP8sciiToUTF8(t *testing.T) {
	// ASCII passes through unchanged.
	if got := p8sciiToUTF8([]byte("hi there\n")); string(got) != "hi there\n" {
		t.Errorf("ascii: got %q", got)
	}
	// Byte 0x94 is the up-arrow glyph, which expands to multi-byte UTF-8.
	if got := p8sciiToUTF8([]byte{0x94}); string(got) != "⬆️" {
		t.Errorf("glyph 0x94: got %q, want %q", got, "⬆️")
	}
}

// Every P8SCII byte must map to something: a gap would silently delete
// characters from extracted source.
func TestP8sciiTableComplete(t *testing.T) {
	for i, s := range p8sciiTable {
		if s == "" {
			t.Errorf("p8sciiTable[%d] is empty", i)
		}
	}
}

// decompressOld indexes the char table with any byte up to 0x3b, so the table
// has to stay exactly 60 entries long for that to be in range.
func TestCompressedLuaCharTableSize(t *testing.T) {
	if got := len(compressedLuaCharTable); got != 60 {
		t.Errorf("table size = %d, want 60", got)
	}
}

func TestExtractCodeUncompressed(t *testing.T) {
	// version 0 forces the uncompressed path; content stops at the first NUL,
	// a trailing newline is appended, and \r becomes a space.
	code := []byte("a\rb\x00ignored")
	got := extractCode(code, 0)
	if want := "a b\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractCodeUncompressedNoTerminator(t *testing.T) {
	// Without a NUL the whole region is code, still newline terminated.
	if got := extractCode([]byte("abc"), 0); string(got) != "abc\n" {
		t.Errorf("got %q, want %q", got, "abc\n")
	}
}

func TestExtractCodeOldRoundTrip(t *testing.T) {
	src := "function _init()\n print(\"hi\")\nend"
	got := extractCode(compressOld([]byte(src)), 8)
	if string(got) != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

func TestExtractCodePXARoundTrip(t *testing.T) {
	// Repeated text drives the move-to-front table through several literal
	// widths as bytes migrate toward the front.
	src := "x=0\nfunction _update60()\n x+=1\n if x>99 then x=0 end\nend"
	got := extractCode(compressPXA([]byte(src)), 41)
	if string(got) != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

func TestDecompressPXARawRun(t *testing.T) {
	// The raw-run form carries bytes verbatim rather than through the table.
	raw := []byte("-- raw bytes")
	w := newPXAWriter()
	w.writeRawRun(raw)
	if got := decompressPXA(w.stream(len(raw))); !bytes.Equal(got, raw) {
		t.Errorf("got %q, want %q", got, raw)
	}
}

func TestDecompressPXABackref(t *testing.T) {
	// Emit "abc" as literals, then copy it back twice: a length of 6 over an
	// offset of 3 also exercises the multi-group length encoding.
	w := newPXAWriter()
	for _, b := range []byte("abc") {
		w.writeLiteral(b)
	}
	w.writeBackref(3, 6)
	if got := decompressPXA(w.stream(9)); string(got) != "abcabcabc" {
		t.Errorf("got %q, want %q", got, "abcabcabc")
	}
}

func TestDecompressOld(t *testing.T) {
	// ":c:" stream: 3 table-literal 'a' bytes (index 13 in the char table).
	code := []byte{':', 'c', ':', 0x00, 0x00, 0x03, 0x00, 0x00, 0x0d, 0x0d, 0x0d}
	if got := decompressOld(code); string(got) != "aaa" {
		t.Errorf("literals: got %q, want %q", got, "aaa")
	}

	// One literal 'a' then a back-reference (offset 1, length 2) copying it.
	// byte 0x3c,0x01 => offset=(0x3c-0x3c)*16+1=1, length=(1>>4)+2=2.
	code = []byte{':', 'c', ':', 0x00, 0x00, 0x03, 0x00, 0x00, 0x0d, 0x3c, 0x01}
	if got := decompressOld(code); string(got) != "aaa" {
		t.Errorf("backref: got %q, want %q", got, "aaa")
	}
}

// sampleCode is a one-line cart body used wherever the source itself does not
// matter.
const sampleCode = "x=1"

func TestStripFutureCode(t *testing.T) {
	// PICO-8 appends the trailer for _update60 back-compat; it and the newline
	// before it are removed.
	code := append([]byte(sampleCode+"\n"), futureCode1...)
	if got := stripFutureCode(code, futureCode1); string(got) != sampleCode {
		t.Errorf("trailer1: got %q, want %q", got, sampleCode)
	}
	code = append([]byte(sampleCode+"\n"), futureCode2...)
	if got := stripFutureCode(code, futureCode2); string(got) != sampleCode {
		t.Errorf("trailer2: got %q, want %q", got, sampleCode)
	}
	// Anything else is returned untouched.
	if got := stripFutureCode([]byte(sampleCode), futureCode1); string(got) != sampleCode {
		t.Errorf("absent: got %q, want %q", got, sampleCode)
	}
}

// Cart data is only ever partly trustworthy: nothing here may panic, and each
// case keeps whatever decoded cleanly.
func TestExtractCodeMalformed(t *testing.T) {
	longPXA := append([]byte{0x00, 'p', 'x', 'a', 0x00, 0x10, 0xff, 0xff}, bytes.Repeat([]byte{0xff}, 16)...)

	tests := []struct {
		name    string
		code    []byte
		version byte
	}{
		{"pxa header only", []byte("\x00pxa"), 1},
		{"old header only", []byte(":c:\x00"), 1},
		{"empty", nil, 1},
		{"pxa compressed size past buffer", longPXA, 1},
		{"pxa unbounded literal width", append([]byte{0x00, 'p', 'x', 'a', 0x00, 0x08, 0x00, 0x18}, bytes.Repeat([]byte{0xff}, 16)...), 1},
		{"old zero offset backref", []byte{':', 'c', ':', 0x00, 0x00, 0x04, 0x00, 0x00, 0x3c, 0x00}, 1},
		{"old backref past output", []byte{':', 'c', ':', 0x00, 0x00, 0x04, 0x00, 0x00, 0x0d, 0x3d, 0x01}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractCode(tt.code, tt.version); bytes.IndexByte(got, '\r') >= 0 {
				t.Error("carriage return survived")
			}
		})
	}
}

func TestExtractCartridgeData(t *testing.T) {
	// One pixel per byte, two bits per channel in ARGB order.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 0x03, G: 0x02, B: 0x01, A: 0x00})
	img.SetNRGBA(1, 0, color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x03})

	got := extractCartridgeData(img)
	want := []byte{0x39, 0xc0}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestExtractCartridgeDataSubImage(t *testing.T) {
	// A sub-image has a non-zero origin; only its own pixels may be read.
	full := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	full.SetNRGBA(0, 0, color.NRGBA{R: 0x03, G: 0x03, B: 0x03, A: 0x03})
	full.SetNRGBA(1, 0, color.NRGBA{R: 0x00, G: 0x00, B: 0x01, A: 0x00})

	sub := full.SubImage(image.Rect(1, 0, 2, 1))
	got := extractCartridgeData(sub)
	if want := []byte{0x01}; !bytes.Equal(got, want) {
		t.Errorf("got %x, want %x", got, want)
	}
}

func TestRomToP8ShortROM(t *testing.T) {
	if got := romToP8(make([]byte, 16), []byte("x=1\n"), 8); got != nil {
		t.Errorf("short rom: got %d bytes, want nil", len(got))
	}
}

func TestRomToP8Sections(t *testing.T) {
	rom := buildROM([]byte("x=1\n"), 8)
	p8 := string(romToP8(rom, []byte("x=1\n"), 8))

	if !strings.HasPrefix(p8, "pico-8 cartridge // http://www.pico-8.com\nversion 8\n__lua__\nx=1\n") {
		t.Errorf("header/lua section wrong: %q", p8[:80])
	}
	for _, section := range []string{"__gfx__", "__gff__", "__map__", "__sfx__", "__music__"} {
		if !strings.Contains(p8, "\n"+section+"\n") {
			t.Errorf("missing section %s", section)
		}
	}
}

func TestRomToP8EmptyLua(t *testing.T) {
	// An empty code section still gets a newline, so __gfx__ starts on its own
	// line rather than running into the source.
	rom := buildROM(nil, 8)
	p8 := string(romToP8(rom, nil, 8))
	if !strings.Contains(p8, "__lua__\n\n__gfx__\n") {
		t.Error("empty lua section is not newline terminated")
	}
}

func TestWriteGfxNibbleSwap(t *testing.T) {
	data := make([]byte, 64)
	data[0] = 0xab
	var b bytes.Buffer
	writeGfx(&b, data)
	line := b.Bytes()
	if len(line) != 129 { // 128 hex chars + newline
		t.Fatalf("line length = %d, want 129", len(line))
	}
	if string(line[:2]) != "ba" {
		t.Errorf("nibble swap: got %q, want %q", line[:2], "ba")
	}
}

func TestWriteMusic(t *testing.T) {
	// begin-loop flag on channel 0; channels carry values with high bit set.
	data := []byte{0x80, 0x01, 0x02, 0x03}
	var b bytes.Buffer
	writeMusic(&b, data)
	if want := "01 00010203\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}

func TestWriteSfx(t *testing.T) {
	data := make([]byte, 68) // all zero
	data[65] = 0x01          // note duration (speed)
	var b bytes.Buffer
	writeSfx(&b, data)
	line := b.Bytes()
	if len(line) != 169 { // 168 hex chars + newline
		t.Fatalf("line length = %d, want 169", len(line))
	}
	if string(line[:8]) != "00010000" { // editor, speed, loopStart, loopEnd
		t.Errorf("header: got %q, want %q", line[:8], "00010000")
	}
}

// Names repeated across the argument table.
const (
	argCart         = "cart.p8.png"
	categoryMap     = "map"
	categorySprites = "sprites"
)

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		only       []string
		positional []string
	}{
		{"no flags", []string{argCart}, nil, []string{argCart}},
		{"double dash", []string{argCart, "--only=map,sprites"}, []string{categoryMap, categorySprites}, []string{argCart}},
		{"single dash", []string{"-only=map"}, []string{categoryMap}, nil},
		{"blanks trimmed", []string{"--only= map , ,sprites "}, []string{categoryMap, categorySprites}, nil},
		{"last flag wins", []string{"--only=" + categoryMap, "--only=sprites"}, []string{categorySprites}, nil},
		{"empty list", []string{"--only="}, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			only, positional := parseArgs(tt.args)
			if strings.Join(only, ",") != strings.Join(tt.only, ",") {
				t.Errorf("only = %v, want %v", only, tt.only)
			}
			if strings.Join(positional, ",") != strings.Join(tt.positional, ",") {
				t.Errorf("positional = %v, want %v", positional, tt.positional)
			}
		})
	}
}

func TestIsDirOutput(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"empty", "", false},
		{"trailing slash", "out/", true},
		{"existing directory", dir, true},
		{"plain filename", "out.p8", false},
		{"missing path", dir + "/nope", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDirOutput(tt.path); got != tt.want {
				t.Errorf("isDirOutput(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestWarnUnknownCategories(t *testing.T) {
	var b bytes.Buffer
	warnUnknownCategories(&b, []string{categoryMap, categorySprites, "metadata", "spritesheet", outputP8})
	if b.Len() != 0 {
		t.Errorf("valid categories warned: %q", b.String())
	}

	b.Reset()
	warnUnknownCategories(&b, []string{"bogus"})
	if !strings.Contains(b.String(), `unknown --only category "bogus"`) {
		t.Errorf("missing warning: %q", b.String())
	}
}

func TestWriteToFile(t *testing.T) {
	// A nested path is created on the way.
	path := t.TempDir() + "/nested/dir/out.txt"
	if err := writeToFile([]byte("hello"), path); err != nil {
		t.Fatalf("writeToFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}

	// Writing under a path that is a file, not a directory, fails.
	if err := writeToFile([]byte("hello"), path+"/child.txt"); err == nil {
		t.Error("expected an error writing beneath a regular file")
	}
}

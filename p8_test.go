package main

import (
	"bytes"
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

func TestExtractCodeUncompressed(t *testing.T) {
	// version 0 forces the uncompressed path; content stops at the first NUL,
	// a trailing newline is appended, and \r becomes a space.
	code := []byte("a\rb\x00ignored")
	got := extractCode(code, 0)
	if want := "a b\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
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

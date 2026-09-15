package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// A real cart image is 160x205, which is just over the 0x8001 bytes a cart
// ROM needs; generated carts use the same shape.
const (
	cartWidth  = 160
	cartHeight = 205
	cartPixels = cartWidth * cartHeight
)

// buildROM assembles a cart ROM the size of a real cart image: the data
// sections below 0x4300 carry a deterministic pattern, code sits at 0x4300
// (NUL padded, so the uncompressed reader stops at its end) and the version
// byte at 0x8000.
func buildROM(code []byte, version byte) []byte {
	rom := make([]byte, cartPixels)
	for i := 0; i < codeStart; i++ {
		rom[i] = byte(i*31 + 7)
	}
	copy(rom[codeStart:codeEnd], code)
	rom[versionAddr] = version
	return rom
}

// writeCartPNG encodes rom into a .p8.png in a temp directory and returns its
// path. Each byte becomes one pixel, packed ARGB two bits per channel, the
// inverse of extractCartridgeData. The high six bits of every channel stay
// zero: alpha then never reaches 255, which keeps png.Encode on the
// non-premultiplied RGBA path a real cart uses, so these fixtures exercise the
// same decoding as one.
func writeCartPNG(t *testing.T, rom []byte) string {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, cartWidth, cartHeight))
	for i := 0; i < cartPixels && i < len(rom); i++ {
		b := rom[i]
		img.SetNRGBA(i%cartWidth, i/cartWidth, color.NRGBA{
			R: (b >> 4) & 0x03,
			G: (b >> 2) & 0x03,
			B: b & 0x03,
			A: (b >> 6) & 0x03,
		})
	}

	path := filepath.Join(t.TempDir(), "generated.p8.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating cart png: %v", err)
	}
	defer f.Close() //nolint:errcheck
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encoding cart png: %v", err)
	}
	return path
}

// compressOld encodes src in the older ":c:" format: a table index for every
// byte the char table covers, and a 0x00-escaped raw literal for the rest.
func compressOld(src []byte) []byte {
	// Index 0 is the escape marker, so entries are registered from the back
	// and '#', which appears at both index 0 and index 40, keeps the latter.
	index := make(map[byte]byte, len(compressedLuaCharTable))
	for i := len(compressedLuaCharTable) - 1; i >= 1; i-- {
		index[compressedLuaCharTable[i]] = byte(i)
	}

	out := []byte{':', 'c', ':', 0x00, byte(len(src) >> 8), byte(len(src)), 0x00, 0x00}
	for _, b := range src {
		if i, ok := index[b]; ok {
			out = append(out, i)
			continue
		}
		out = append(out, 0x00, b)
	}
	return out
}

// pxaWriter builds a "\x00pxa" bit stream. decompressPXA reads bits LSB-first
// within each byte, so bits are collected one per entry and packed at the end.
type pxaWriter struct {
	bits  []byte
	state [256]byte
}

func newPXAWriter() *pxaWriter {
	w := &pxaWriter{}
	for i := range w.state {
		w.state[i] = byte(i)
	}
	return w
}

func (w *pxaWriter) writeBit(v uint32) { w.bits = append(w.bits, byte(v&1)) }

func (w *pxaWriter) writeBits(v uint32, count int) {
	for i := 0; i < count; i++ {
		w.writeBit(v >> uint(i))
	}
}

// writeLiteral emits one byte through the move-to-front table, mirroring the
// table updates decompressPXA makes as it reads. An index is encoded with the
// smallest width nbits >= 4 whose range [2^nbits-16, 2^(nbits+1)-17] covers
// it, announced as nbits-4 one bits followed by a zero.
func (w *pxaWriter) writeLiteral(ch byte) {
	idx := 0
	for w.state[idx] != ch {
		idx++
	}

	nbits := 4
	for idx > (1<<(nbits+1))-17 {
		nbits++
	}

	w.writeBit(1)
	for i := 4; i < nbits; i++ {
		w.writeBit(1)
	}
	w.writeBit(0)
	w.writeBits(uint32(idx-(1<<nbits)+16), nbits)

	copy(w.state[1:idx+1], w.state[:idx])
	w.state[0] = ch
}

// writeBackref emits a back-reference copying length bytes from offset bytes
// earlier. The length is carried as 3-bit groups over a base of 3, where a
// group of 7 means "continue" - so a length whose remainder is a multiple of
// seven needs a trailing zero group to terminate.
func (w *pxaWriter) writeBackref(offset, length int) {
	w.writeBit(0)
	w.writeBit(1)
	w.writeBit(1) // nbits = 5
	w.writeBits(uint32(offset-1), 5)

	remaining := length - 3
	for remaining >= 7 {
		w.writeBits(7, 3)
		remaining -= 7
	}
	w.writeBits(uint32(remaining), 3)
}

// writeRawRun emits data verbatim: the 10-bit offset form with an offset of 1
// switches the decoder into a NUL-terminated run of raw bytes.
func (w *pxaWriter) writeRawRun(data []byte) {
	w.writeBit(0)
	w.writeBit(1)
	w.writeBit(0) // nbits = 10
	w.writeBits(0, 10)
	for _, b := range data {
		w.writeBits(uint32(b), 8)
	}
	w.writeBits(0, 8)
}

// stream packs the accumulated bits behind a header declaring length
// decompressed bytes and the total stream size, header included.
func (w *pxaWriter) stream(length int) []byte {
	body := make([]byte, (len(w.bits)+7)/8)
	for i, bit := range w.bits {
		if bit != 0 {
			body[i>>3] |= 1 << (i & 7)
		}
	}

	out := make([]byte, headerLen, headerLen+len(body))
	copy(out, pxaHeader)
	out[4] = byte(length >> 8)
	out[5] = byte(length)
	total := headerLen + len(body)
	out[6] = byte(total >> 8)
	out[7] = byte(total)
	return append(out, body...)
}

// compressPXA encodes src as a literal-only "\x00pxa" stream.
func compressPXA(src []byte) []byte {
	w := newPXAWriter()
	for _, b := range src {
		w.writeLiteral(b)
	}
	return w.stream(len(src))
}

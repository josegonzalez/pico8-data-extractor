package main

import (
	"bytes"
	"strconv"
)

const hexdigits = "0123456789abcdef"

// p8sciiToUTF8 converts raw P8SCII bytes (e.g. decompressed Lua source) into
// their UTF-8 representation. Bytes 0x20-0x7e pass through as ASCII; PICO-8
// glyphs and control codes expand to their Unicode equivalents.
func p8sciiToUTF8(code []byte) []byte {
	var b bytes.Buffer
	b.Grow(len(code))
	for _, c := range code {
		b.WriteString(p8sciiTable[c])
	}
	return b.Bytes()
}

func writeHexByte(b *bytes.Buffer, v byte) {
	b.WriteByte(hexdigits[v>>4])
	b.WriteByte(hexdigits[v&0x0f])
}

// romToP8 renders a 32KB cart ROM (plus its version byte) and the already
// decompressed Lua code into .p8 text, byte-compatible with picotool.
func romToP8(rom, code []byte, version byte) []byte {
	var b bytes.Buffer

	b.WriteString("pico-8 cartridge // http://www.pico-8.com\n")
	b.WriteString("version ")
	b.WriteString(strconv.Itoa(int(version)))
	b.WriteByte('\n')

	// __lua__: verbatim source, ending in a newline.
	b.WriteString("__lua__\n")
	lua := p8sciiToUTF8(code)
	b.Write(lua)
	if len(lua) == 0 || lua[len(lua)-1] != '\n' {
		b.WriteByte('\n')
	}

	// __gfx__: 128 rows x 64 bytes, nibbles swapped within each byte.
	b.WriteString("__gfx__\n")
	writeGfx(&b, rom[0x0000:0x2000])

	// (No __label__: the label lives in the visible PNG, not the ROM.)

	// PICO-8 emits a blank line before __gff__.
	b.WriteByte('\n')
	b.WriteString("__gff__\n")
	writeHexLines(&b, rom[0x3000:0x3100], 128)

	b.WriteString("__map__\n")
	writeHexLines(&b, rom[0x2000:0x3000], 128)

	b.WriteString("__sfx__\n")
	writeSfx(&b, rom[0x3200:0x4300])

	b.WriteString("__music__\n")
	writeMusic(&b, rom[0x3100:0x3200])

	// Trailing blank line.
	b.WriteByte('\n')
	return b.Bytes()
}

// writeGfx writes the sprite sheet: each byte's nibbles are swapped so the text
// reads left-to-right in pixel order.
func writeGfx(b *bytes.Buffer, data []byte) {
	const bytesPerLine = 64
	for i := 0; i+bytesPerLine <= len(data); i += bytesPerLine {
		for _, v := range data[i : i+bytesPerLine] {
			writeHexByte(b, (v&0x0f)<<4|(v&0xf0)>>4)
		}
		b.WriteByte('\n')
	}
}

// writeHexLines writes straight lowercase hex, bytesPerLine bytes per line.
func writeHexLines(b *bytes.Buffer, data []byte, bytesPerLine int) {
	for i := 0; i+bytesPerLine <= len(data); i += bytesPerLine {
		for _, v := range data[i : i+bytesPerLine] {
			writeHexByte(b, v)
		}
		b.WriteByte('\n')
	}
}

// writeSfx writes 64 sound-effect patterns (68 ROM bytes each) as 168-hex-char
// lines: an 8-char header then 32 notes of 5 hex chars (pitch, waveform|volume,
// effect nibble).
func writeSfx(b *bytes.Buffer, data []byte) {
	const patternBytes = 68
	for base := 0; base+patternBytes <= len(data); base += patternBytes {
		p := data[base : base+patternBytes]
		writeHexByte(b, p[64]) // editor mode
		writeHexByte(b, p[65]) // note duration (speed)
		writeHexByte(b, p[66]) // loop start
		writeHexByte(b, p[67]) // loop end
		for n := 0; n < 32; n++ {
			lsb, msb := p[n*2], p[n*2+1]
			pitch := lsb & 0x3f
			waveform := ((msb & 0x80) >> 4) | ((msb & 0x01) << 2) | ((lsb & 0xc0) >> 6)
			volume := (msb & 0x0e) >> 1
			effect := (msb & 0x70) >> 4
			writeHexByte(b, pitch)
			writeHexByte(b, waveform<<4|volume)
			b.WriteByte(hexdigits[effect&0x0f])
		}
		b.WriteByte('\n')
	}
}

// writeMusic writes 64 song patterns (4 ROM bytes each) as "FF CCCCCCCC" lines:
// a flags byte (built from the high bits of the first three channels) and the
// four channel bytes with their high bit masked off.
func writeMusic(b *bytes.Buffer, data []byte) {
	const patternBytes = 4
	for base := 0; base+patternBytes <= len(data); base += patternBytes {
		c0, c1, c2, c3 := data[base], data[base+1], data[base+2], data[base+3]
		fnext := (c0 & 0x80) >> 7
		frepeat := (c1 & 0x80) >> 7
		fstop := (c2 & 0x80) >> 7
		writeHexByte(b, (fstop<<2)|(frepeat<<1)|fnext)
		b.WriteByte(' ')
		writeHexByte(b, c0&0x7f)
		writeHexByte(b, c1&0x7f)
		writeHexByte(b, c2&0x7f)
		writeHexByte(b, c3&0x7f)
		b.WriteByte('\n')
	}
}

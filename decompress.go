package main

import "bytes"

// PICO-8 code-section compression headers.
var (
	pxaHeader = []byte{0x00, 'p', 'x', 'a'}
	oldHeader = []byte{':', 'c', ':', 0x00}
)

// compressedLuaCharTable is the 60-entry lookup used by the old ":c:" format;
// literal indices 0x01..0x3b map directly to these bytes.
var compressedLuaCharTable = []byte("#\n 0123456789abcdefghijklmnopqrstuvwxyz!#%(){}[]<>+=/*:;.,~_")

// PICO-8 appends one of these "future code" trailers before compressing (for
// _update60 back-compat) and strips it on load; the old format decompressor
// removes it to recover the original source.
var (
	futureCode1 = []byte("if(_update60)_update=function()_update60()_update60()end")
	futureCode2 = []byte("if(_update60)_update=function()_update60()_update_buttons()_update60()end")
)

// extractCode returns the decompressed Lua source for the raw code region
// (rom[0x4300:0x8000]) given the cart version byte. It detects the PXA and old
// ":c:" compression formats and falls back to treating the region as
// uncompressed ASCII. Carriage returns are normalized to spaces, matching
// PICO-8 / picotool.
func extractCode(code []byte, version byte) []byte {
	var out []byte
	switch {
	case version != 0 && bytes.HasPrefix(code, pxaHeader):
		out = decompressPXA(code)
	case version != 0 && bytes.HasPrefix(code, oldHeader):
		out = decompressOld(code)
	default:
		out = uncompressedCode(code)
	}
	for i, b := range out {
		if b == '\r' {
			out[i] = ' '
		}
	}
	return out
}

// uncompressedCode returns the raw ASCII code up to the first NUL byte, with a
// trailing newline appended (matching picotool's handling).
func uncompressedCode(code []byte) []byte {
	n := bytes.IndexByte(code, 0)
	if n < 0 {
		n = len(code)
	}
	out := make([]byte, n+1)
	copy(out, code[:n])
	out[n] = '\n'
	return out
}

// decompressPXA decompresses the newer "\x00pxa" format. Port of the zepto8 /
// fake-08 pxa_decompress: a bit-oriented LZ scheme with a move-to-front table.
func decompressPXA(input []byte) []byte {
	length := int(input[4])*256 + int(input[5])
	compressed := int(input[6])*256 + int(input[7])

	pos := 8 * 8 // stream position in bits
	getBits := func(count int) uint32 {
		var n uint32
		for i := 0; i < count && pos < compressed*8; i, pos = i+1, pos+1 {
			n |= uint32((input[pos>>3]>>(pos&7))&1) << i
		}
		return n
	}

	// Move-to-front table, initialized to the identity permutation.
	var state [256]byte
	for i := range state {
		state[i] = byte(i)
	}
	mtfGet := func(n int) byte {
		ch := state[n]
		copy(state[1:n+1], state[:n])
		state[0] = ch
		return ch
	}

	ret := make([]byte, 0, length)
	for len(ret) < length && pos < compressed*8 {
		if getBits(1) != 0 {
			nbits := 4
			for getBits(1) != 0 {
				nbits++
			}
			n := int(getBits(nbits)) + (1 << nbits) - 16
			ch := mtfGet(n)
			if ch == 0 {
				break
			}
			ret = append(ret, ch)
			continue
		}

		var nbits int
		if getBits(1) != 0 {
			if getBits(1) != 0 {
				nbits = 5
			} else {
				nbits = 10
			}
		} else {
			nbits = 15
		}
		offset := int(getBits(nbits)) + 1

		if nbits == 10 && offset == 1 {
			// Run of raw bytes, terminated by a NUL.
			for ch := byte(getBits(8)); ch != 0; ch = byte(getBits(8)) {
				ret = append(ret, ch)
			}
			continue
		}

		ln := 3
		for {
			n := int(getBits(3))
			ln += n
			if n != 7 {
				break
			}
		}
		for i := 0; i < ln; i++ {
			ret = append(ret, ret[len(ret)-offset])
		}
	}
	return ret
}

// decompressOld decompresses the older ":c:\x00" format. Port of picotool's
// decompress_code: single-byte table literals, 0x00-escaped raw literals, and
// two-byte LZ back-references.
func decompressOld(code []byte) []byte {
	codeLength := int(code[4])<<8 | int(code[5])

	out := make([]byte, 0, codeLength)
	inI := 8
	for len(out) < codeLength && inI < len(code) {
		b := code[inI]
		switch {
		case b == 0x00:
			inI++
			if inI < len(code) {
				out = append(out, code[inI])
			}
		case b <= 0x3b:
			out = append(out, compressedLuaCharTable[b])
		default:
			inI++
			if inI >= len(code) {
				break
			}
			b2 := code[inI]
			offset := (int(b)-0x3c)*16 + int(b2&0x0f)
			length := int(b2>>4) + 2
			for i := 0; i < length; i++ {
				out = append(out, out[len(out)-offset])
			}
		}
		inI++
	}

	out = bytes.Trim(out, "\x00")
	out = stripFutureCode(out, futureCode1)
	out = stripFutureCode(out, futureCode2)
	return out
}

// stripFutureCode removes a trailing future-code trailer (and a newline that
// immediately precedes it) if present.
func stripFutureCode(code, trailer []byte) []byte {
	if !bytes.HasSuffix(code, trailer) {
		return code
	}
	code = code[:len(code)-len(trailer)]
	if len(code) > 0 && code[len(code)-1] == '\n' {
		code = code[:len(code)-1]
	}
	return code
}

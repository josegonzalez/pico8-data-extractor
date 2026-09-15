// Package main reads PICO-8 .p8.png cartridges: it decodes the ROM embedded in
// the image, decompresses the Lua code section, and writes the code, the full
// .p8 text cart, or the extracted cart data.
package main

import "bytes"

// PICO-8 code-section compression headers.
var (
	pxaHeader = []byte{0x00, 'p', 'x', 'a'}
	oldHeader = []byte{':', 'c', ':', 0x00}
)

// headerLen is the size of both compression headers, including the two
// big-endian length fields that follow the 4-byte magic.
const headerLen = 8

// maxLiteralBits caps the unary-coded bit width of a PXA literal index. The
// widest index the format can express (255) needs 8 bits, so anything longer
// is corrupt data rather than a literal.
const maxLiteralBits = 8

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
// PICO-8 / picotool. Malformed data yields whatever decoded cleanly rather
// than an error: a cart is only ever partly trustworthy.
func extractCode(code []byte, version byte) []byte {
	var out []byte
	switch {
	case version != 0 && len(code) >= headerLen && bytes.HasPrefix(code, pxaHeader):
		out = decompressPXA(code)
	case version != 0 && len(code) >= headerLen && bytes.HasPrefix(code, oldHeader):
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
	if len(input) < headerLen {
		return nil
	}
	length := int(input[4])*256 + int(input[5])
	compressed := int(input[6])*256 + int(input[7])
	// The header counts the whole stream, itself included; a cart claiming
	// more than it carries must not read past the buffer.
	if compressed > len(input) {
		compressed = len(input)
	}

	pos := headerLen * 8 // stream position in bits
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
				if nbits > maxLiteralBits {
					return ret
				}
			}
			n := int(getBits(nbits)) + (1 << nbits) - 16
			if n < 0 || n >= len(state) {
				return ret
			}
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

		// A back-reference reaching behind the start of the output means the
		// stream is corrupt; keep what decoded so far.
		if offset > len(ret) {
			return ret
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
	if len(code) < headerLen {
		return nil
	}
	codeLength := int(code[4])<<8 | int(code[5])

	out := make([]byte, 0, codeLength)
	inI := headerLen
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
			// A zero or over-long offset would read outside the output;
			// keep what decoded so far.
			if offset == 0 || offset > len(out) {
				return trimDecompressed(out)
			}
			for i := 0; i < length; i++ {
				out = append(out, out[len(out)-offset])
			}
		}
		inI++
	}

	return trimDecompressed(out)
}

// trimDecompressed strips the NUL padding and the future-code trailers PICO-8
// adds around ":c:" compressed source.
func trimDecompressed(out []byte) []byte {
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

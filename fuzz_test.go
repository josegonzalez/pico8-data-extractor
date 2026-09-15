package main

import (
	"bytes"
	"testing"
)

// FuzzExtractCode drives the code-section reader with arbitrary bytes. A cart
// is user-supplied data, so no input may panic, whatever its headers claim.
// The seed corpus alone runs under an ordinary go test, which is what keeps
// the bounds guards honest in CI.
func FuzzExtractCode(f *testing.F) {
	const source = "-- fuzz\nfunction _init()\n x=1\nend\n"

	// The three formats, decoded the way a real cart would be.
	f.Add([]byte(source), byte(0))
	f.Add(compressOld([]byte(source)), byte(8))
	f.Add(compressPXA([]byte(source)), byte(41))

	// Headers that promise more than they carry.
	f.Add([]byte("\x00pxa"), byte(1))
	f.Add([]byte(":c:\x00"), byte(1))
	f.Add(append([]byte{0x00, 'p', 'x', 'a', 0x00, 0x10, 0xff, 0xff}, bytes.Repeat([]byte{0xff}, 16)...), byte(1))

	// Back-references pointing outside the output produced so far.
	f.Add([]byte{':', 'c', ':', 0x00, 0x00, 0x04, 0x00, 0x00, 0x3c, 0x00}, byte(1))
	f.Add([]byte{':', 'c', ':', 0x00, 0x00, 0x04, 0x00, 0x00, 0x0d, 0x3d, 0x01}, byte(1))

	f.Fuzz(func(t *testing.T, code []byte, version byte) {
		out := extractCode(code, version)

		// Carriage returns are normalized away on every path.
		if i := bytes.IndexByte(out, '\r'); i >= 0 {
			t.Errorf("carriage return survived at offset %d", i)
		}
	})
}

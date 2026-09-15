package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/josegonzalez/parsepico/pico8"
)

const (
	// PICO-8 cartridge data offsets
	codeStart   = 0x4300
	codeEnd     = 0x8000
	versionAddr = 0x8000
)

// defaultProgName names the tool in the usage text when the process was given
// no argv[0] to report.
const defaultProgName = "pico8-data-extractor"

// usageText is the help shown when no input file is given. The single
// placeholder is the program name.
const usageText = `Usage: %s <p8.png file> [output] [--only=cat,...]
If the output is a directory (ends in / or exists), the full cart data is
extracted into it (sprites, spritesheet, map, JSON). An output ending in
.p8 writes the cart as .p8 text. Otherwise the Lua code is written to the
file, or to stdout if none is given.
For a directory output, --only limits which categories are written
(comma-separated): metadata, spritesheet, sprites, map, p8.
`

func main() {
	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

// printf writes a message to w, discarding the write error: failing to report
// something is not itself worth reporting.
func printf(w io.Writer, format string, args ...any) {
	fmt.Fprintf(w, format, args...) //nolint:errcheck
}

// run carries out one invocation of the CLI and returns the process exit code.
// args is the full argument vector, program name included, and every message
// goes to the given writers rather than the process streams so that the whole
// command is testable.
func run(args []string, stdout, stderr io.Writer) int {
	prog := defaultProgName
	if len(args) > 0 {
		prog = args[0]
	}
	var argv []string
	if len(args) > 1 {
		argv = args[1:]
	}

	only, positional := parseArgs(argv)
	if len(positional) < 1 {
		printf(stderr, usageText, prog)
		return 1
	}
	warnUnknownCategories(stderr, only)

	inputFile := positional[0]
	var outputFile string
	if len(positional) > 1 {
		outputFile = positional[1]
	}

	// Open and decode the PNG file
	file, err := os.Open(inputFile)
	if err != nil {
		printf(stderr, "Error opening file %s: %v\n", inputFile, err)
		return 1
	}
	defer file.Close() //nolint:errcheck

	img, err := png.Decode(file)
	if err != nil {
		printf(stderr, "Error decoding PNG: %v\n", err)
		return 1
	}

	// Extract the embedded cartridge ROM
	rom := extractCartridgeData(img)
	if len(rom) <= versionAddr {
		printf(stderr, "Error: cartridge data too small: expected more than %d bytes, got %d\n", versionAddr, len(rom))
		return 1
	}

	// Decompress the Lua code section
	version := rom[versionAddr]
	code := extractCode(rom[codeStart:codeEnd], version)

	// A directory output means "extract everything with parsepico".
	if isDirOutput(outputFile) {
		if err := extractAll(inputFile, outputFile, rom, code, version, only); err != nil {
			printf(stderr, "Error extracting cart data: %v\n", err)
			return 1
		}
		printf(stdout, "Extracted cart data into %s\n", outputFile)
		return 0
	}

	// A .p8 output filename means "write the whole cart as .p8 text".
	if strings.HasSuffix(strings.ToLower(outputFile), ".p8") {
		p8 := romToP8(rom, code, version)
		if err := writeToFile(p8, outputFile); err != nil {
			printf(stderr, "Error writing to file %s: %v\n", outputFile, err)
			return 1
		}
		printf(stdout, "Cart converted and saved to %s\n", outputFile)
		return 0
	}

	// Otherwise emit the Lua code (to the given file, or stdout).
	lua := p8sciiToUTF8(code)
	if outputFile != "" {
		if err := writeToFile(lua, outputFile); err != nil {
			printf(stderr, "Error writing to file %s: %v\n", outputFile, err)
			return 1
		}
		printf(stdout, "Lua code extracted and saved to %s\n", outputFile)
		return 0
	}
	stdout.Write(lua) //nolint:errcheck
	return 0
}

// extractCartridgeData extracts the embedded PICO-8 cartridge data from the PNG image
func extractCartridgeData(img image.Image) []byte {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// PICO-8 stores data in the 2 least significant bits of each color channel,
	// including alpha. Read the raw, non-premultiplied channel values (NRGBA):
	// image.Image.RGBA() would alpha-premultiply and corrupt those low bits,
	// since cart pixels are typically not fully opaque.
	data := make([]byte, 0, width*height)

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)

			// Extract 2 least significant bits from each channel and combine into one byte
			// Format: ARGB (2 bits each)
			byteVal := (c.A&0x03)<<6 | (c.R&0x03)<<4 | (c.G&0x03)<<2 | (c.B & 0x03)
			data = append(data, byteVal)
		}
	}

	return data
}

// isDirOutput reports whether the output path should be treated as a directory
// to extract cart data into (it ends with a path separator, or already exists
// as a directory).
func isDirOutput(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasSuffix(path, "/") || strings.HasSuffix(path, string(os.PathSeparator)) {
		return true
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return true
	}
	return false
}

// extractAll converts the cart to .p8 text inside outDir, then uses parsepico
// to extract sprites, the spritesheet, the map, and JSON metadata into outDir.
// only limits which categories are produced (empty means all); the "p8"
// category controls whether the intermediate .p8 is kept.
func extractAll(inputFile, outDir string, rom, code []byte, version byte, only []string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}

	// Name the intermediate .p8 after the input cart (foo.p8.png -> foo.p8).
	base := strings.TrimSuffix(filepath.Base(inputFile), ".png")
	if !strings.HasSuffix(base, ".p8") {
		base += ".p8"
	}
	p8Path := filepath.Join(outDir, base)
	if err := writeToFile(romToP8(rom, code, version), p8Path); err != nil {
		return err
	}

	// Split the "p8" category (handled here) from the library categories.
	var libOnly []string
	keepP8 := len(only) == 0
	for _, c := range only {
		if c == outputP8 {
			keepP8 = true
		} else {
			libOnly = append(libOnly, c)
		}
	}

	// Run the library extraction unless a filter was given that selects no
	// library categories (e.g. --only=p8).
	if len(only) == 0 || len(libOnly) > 0 {
		if err := pico8.Extract(p8Path, outDir, pico8.Options{Only: libOnly}); err != nil {
			return err
		}
	}

	if !keepP8 {
		return os.Remove(p8Path)
	}
	return nil
}

// parseArgs splits CLI arguments into the --only category list and the
// positional arguments.
func parseArgs(args []string) (only, positional []string) {
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--only="):
			only = splitCategories(strings.TrimPrefix(a, "--only="))
		case strings.HasPrefix(a, "-only="):
			only = splitCategories(strings.TrimPrefix(a, "-only="))
		default:
			positional = append(positional, a)
		}
	}
	return only, positional
}

// splitCategories splits a comma-separated category list, trimming blanks.
func splitCategories(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// outputP8 is the --only category for the converted .p8 text, which this tool
// produces itself rather than via parsepico.
const outputP8 = "p8"

// warnUnknownCategories prints a warning for any unrecognized --only category.
func warnUnknownCategories(w io.Writer, only []string) {
	valid := map[string]bool{
		pico8.OutputMetadata:    true,
		pico8.OutputSpritesheet: true,
		pico8.OutputSprites:     true,
		pico8.OutputMap:         true,
		outputP8:                true,
	}
	for _, c := range only {
		if !valid[c] {
			printf(w, "warning: unknown --only category %q (valid: metadata, spritesheet, sprites, map, p8)\n", c)
		}
	}
}

// writeToFile writes data to a file
func writeToFile(data []byte, filename string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(filename)
	if dir != "." {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck

	_, err = file.Write(data)
	return err
}

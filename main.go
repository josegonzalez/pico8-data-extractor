package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
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

func main() {
	only, positional := parseArgs(os.Args[1:])
	if len(positional) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s <p8.png file> [output] [--only=cat,...]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "If the output is a directory (ends in / or exists), the full cart data is\n")
		fmt.Fprintf(os.Stderr, "extracted into it (sprites, spritesheet, map, JSON). An output ending in\n")
		fmt.Fprintf(os.Stderr, ".p8 writes the cart as .p8 text. Otherwise the Lua code is written to the\n")
		fmt.Fprintf(os.Stderr, "file, or to stdout if none is given.\n")
		fmt.Fprintf(os.Stderr, "For a directory output, --only limits which categories are written\n")
		fmt.Fprintf(os.Stderr, "(comma-separated): metadata, spritesheet, sprites, map, p8.\n")
		os.Exit(1)
	}
	warnUnknownCategories(only)

	inputFile := positional[0]
	var outputFile string
	if len(positional) > 1 {
		outputFile = positional[1]
	}

	// Open and decode the PNG file
	file, err := os.Open(inputFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening file %s: %v\n", inputFile, err)
		os.Exit(1)
	}
	defer file.Close() //nolint:errcheck

	img, err := png.Decode(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding PNG: %v\n", err)
		os.Exit(1)
	}

	// Extract the embedded cartridge ROM
	rom, err := extractCartridgeData(img)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error extracting cartridge data: %v\n", err)
		os.Exit(1)
	}
	if len(rom) <= versionAddr {
		fmt.Fprintf(os.Stderr, "Error: cartridge data too small: expected more than %d bytes, got %d\n", versionAddr, len(rom))
		os.Exit(1)
	}

	// Decompress the Lua code section
	version := rom[versionAddr]
	code := extractCode(rom[codeStart:codeEnd], version)

	// A directory output means "extract everything with parsepico".
	if isDirOutput(outputFile) {
		if err := extractAll(inputFile, outputFile, rom, code, version, only); err != nil {
			fmt.Fprintf(os.Stderr, "Error extracting cart data: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Extracted cart data into %s\n", outputFile)
		return
	}

	// A .p8 output filename means "write the whole cart as .p8 text".
	if strings.HasSuffix(strings.ToLower(outputFile), ".p8") {
		p8 := romToP8(rom, code, version)
		if err := writeToFile(p8, outputFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file %s: %v\n", outputFile, err)
			os.Exit(1)
		}
		fmt.Printf("Cart converted and saved to %s\n", outputFile)
		return
	}

	// Otherwise emit the Lua code (to the given file, or stdout).
	lua := p8sciiToUTF8(code)
	if outputFile != "" {
		if err := writeToFile(lua, outputFile); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing to file %s: %v\n", outputFile, err)
			os.Exit(1)
		}
		fmt.Printf("Lua code extracted and saved to %s\n", outputFile)
		return
	}
	fmt.Print(string(lua))
}

// extractCartridgeData extracts the embedded PICO-8 cartridge data from the PNG image
func extractCartridgeData(img image.Image) ([]byte, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// PICO-8 stores data in the 2 least significant bits of each color channel,
	// including alpha. Read the raw, non-premultiplied channel values (NRGBA):
	// image.Image.RGBA() would alpha-premultiply and corrupt those low bits,
	// since cart pixels are typically not fully opaque.
	var data []byte

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)

			// Extract 2 least significant bits from each channel and combine into one byte
			// Format: ARGB (2 bits each)
			byteVal := (c.A&0x03)<<6 | (c.R&0x03)<<4 | (c.G&0x03)<<2 | (c.B & 0x03)
			data = append(data, byteVal)
		}
	}

	return data, nil
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
		if c == "p8" {
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

// warnUnknownCategories prints a warning for any unrecognized --only category.
func warnUnknownCategories(only []string) {
	valid := map[string]bool{
		pico8.OutputMetadata:    true,
		pico8.OutputSpritesheet: true,
		pico8.OutputSprites:     true,
		pico8.OutputMap:         true,
		"p8":                    true,
	}
	for _, c := range only {
		if !valid[c] {
			fmt.Fprintf(os.Stderr, "warning: unknown --only category %q (valid: metadata, spritesheet, sprites, map, p8)\n", c)
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

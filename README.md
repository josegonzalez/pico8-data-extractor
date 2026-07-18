# PICO-8 Data Extractor

A Go program that reads PICO-8 `.p8.png` files: it extracts the Lua code, converts carts to `.p8` text, or extracts the full cart data (sprites, spritesheet, map, and JSON metadata).

## Overview

PICO-8 `.p8.png` files are PNG images that contain embedded PICO-8 cartridge data, including Lua code, sprites, maps, and audio. This tool extracts the Lua code, converts a `.p8.png` cart into a `.p8` text cart, or extracts the full cart data into a directory.

## Features

- Extracts Lua code from PICO-8 `.p8.png` files
- Converts `.p8.png` carts to `.p8` text (Lua, gfx, gff, map, sfx, music)
- Extracts full cart data (sprite PNGs, spritesheet, map, and JSON) via the [parsepico](https://github.com/josegonzalez/parsepico) library
- Handles both compression formats (the newer `\x00pxa` and the older `:c:`) and uncompressed code
- The core conversion uses only the Go standard library

## Usage

```bash
# Extract Lua code and print to stdout
go run . games/Celeste.p8.png

# Extract Lua code and save to a file
go run . games/Celeste.p8.png output.lua

# Convert the cart to .p8 text (output filename ending in .p8)
go run . games/Celeste.p8.png Celeste.p8

# Extract the full cart data into a directory (output ends in / or is a directory)
go run . games/Celeste.p8.png celeste/

# Extract only specific categories into the directory
go run . games/Celeste.p8.png celeste/ --only=metadata,map
```

The behavior is inferred from the output argument: a directory (ending in `/` or an existing directory) extracts the full cart data into it; an output ending in `.p8` writes the cart as `.p8` text; otherwise the Lua code is written.

For a directory output, `--only` limits which categories are written (comma-separated): `metadata` (metadata.json), `spritesheet` (spritesheet.png/json and section images), `sprites` (the individual sprite PNGs), `map` (map.png/json), and `p8` (the converted `.p8` text). Unselected categories are skipped entirely rather than generated and discarded.

## How it works

1. **PNG Decoding**: Uses Go's standard `image/png` package to decode the PNG file
2. **Data Extraction**: Reads the embedded 32KB ROM from the low 2 bits of each pixel's RGBA channels (non-premultiplied)
3. **Lua Code Location**: Locates the Lua code section at offset `0x4300` to `0x8000`
4. **Decompression**: Detects and decompresses the `\x00pxa` and `:c:` formats (or reads uncompressed code)
5. **Output**: Writes the Lua code, the full `.p8` cart text (header, `__lua__`, `__gfx__`, `__gff__`, `__map__`, `__sfx__`, `__music__`), or - for a directory output - the converted `.p8` plus the extracted sprites, spritesheet, map, and JSON produced by the parsepico library

## File Format

PICO-8 `.p8.png` files store cartridge data using steganography in the least significant bits of the PNG pixel data. The Lua code is typically stored at specific offsets within this embedded data.

## Building

```bash
go build -o pico8-extractor .
```

## Testing

The project includes a sample Celeste `.p8.png` file for testing:

```bash
go run . games/Celeste.p8.png
```

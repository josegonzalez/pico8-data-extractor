# PICO-8 Data Extractor

[![CI](https://github.com/josegonzalez/pico8-data-extractor/actions/workflows/ci.yml/badge.svg)](https://github.com/josegonzalez/pico8-data-extractor/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/josegonzalez/pico8-data-extractor)](https://github.com/josegonzalez/pico8-data-extractor/releases/latest)

A Go program that reads PICO-8 `.p8.png` files: it extracts the Lua code, converts carts to `.p8` text, or extracts the full cart data (sprites, spritesheet, map, and JSON metadata).

## Overview

PICO-8 `.p8.png` files are PNG images that contain embedded PICO-8 cartridge data, including Lua code, sprites, maps, and audio. This tool extracts the Lua code, converts a `.p8.png` cart into a `.p8` text cart, or extracts the full cart data into a directory.

## Features

- Extracts Lua code from PICO-8 `.p8.png` files
- Converts `.p8.png` carts to `.p8` text (Lua, gfx, gff, map, sfx, music)
- Extracts full cart data (sprite PNGs, spritesheet, map, and JSON) via the [parsepico](https://github.com/josegonzalez/parsepico) library
- Handles both compression formats (the newer `\x00pxa` and the older `:c:`) and uncompressed code
- The core conversion uses only the Go standard library
- Ships static, dependency-free binaries for Linux (x86-64, ARMv7, ARM64), macOS, and Windows

## Installation

Every release publishes static binaries for Linux, macOS, and Windows on the [releases page](https://github.com/josegonzalez/pico8-data-extractor/releases). They have no runtime dependencies, so a binary can be copied straight onto a device.

Pick the binary by what `uname -m` reports rather than by device name, since the same handheld ships both 32- and 64-bit firmware:

| `uname -m` | Binary |
| --- | --- |
| `aarch64` / `arm64` | `pico8-data-extractor-linux-arm64` |
| `armv7l` / `armhf` | `pico8-data-extractor-linux-arm` |
| `x86_64` | `pico8-data-extractor-linux-amd64` |

```bash
curl -fsSLO https://github.com/josegonzalez/pico8-data-extractor/releases/latest/download/pico8-data-extractor-linux-arm64
chmod +x pico8-data-extractor-linux-arm64
./pico8-data-extractor-linux-arm64 --version
```

Each release also ships a `checksums.txt` to verify a download against, and the binaries carry a [build provenance attestation](https://github.com/josegonzalez/pico8-data-extractor/attestations) that can be checked with `gh attestation verify`.

The 32-bit ARM binary is built for ARMv7, which covers the Miyoo Mini and its ARMv7 siblings. On macOS, clear the download quarantine with `xattr -d com.apple.quarantine pico8-data-extractor-darwin-arm64` before the binary will run.

## Usage

```bash
# Extract Lua code and print to stdout
go run . testdata/celeste.p8.png

# Extract Lua code and save to a file
go run . testdata/celeste.p8.png output.lua

# Convert the cart to .p8 text (output filename ending in .p8)
go run . testdata/celeste.p8.png celeste.p8

# Extract the full cart data into a directory (output ends in / or is a directory)
go run . testdata/celeste.p8.png celeste/

# Extract only specific categories into the directory
go run . testdata/celeste.p8.png celeste/ --only=metadata,map

# Print the version and exit
go run . --version
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
make build
```

To cross-compile the whole release matrix into `dist/`, alongside a `checksums.txt`:

```bash
make build-release VERSION=0.1.0
```

That covers `linux/amd64`, `linux/arm` (ARMv7), `linux/arm64`, `darwin/amd64`, `darwin/arm64`, and `windows/amd64`. Every binary is built with `CGO_ENABLED=0`, so it is statically linked, and `VERSION` is what `--version` reports.

## Releasing

Releases are cut by hand. Run the [release workflow](https://github.com/josegonzalez/pico8-data-extractor/actions/workflows/release.yaml) from the Actions tab and choose a bump type of `patch`, `minor`, or `major`. The workflow derives the next version from the most recent semver tag, cross-compiles the matrix, and creates the tag and the GitHub release with the binaries attached. Nothing needs to be tagged beforehand.

## Testing

```bash
go test ./...
```

The suite cross-compiles every release target to check that the binaries really are static and built for the machine they are named for. That is the slowest test by far, and `go test -short ./...` skips it.

The rest of the suite runs the whole pipeline against real carts committed under `testdata/`: three from picotool, each with the `.p8` text it was built from as reference output, and Celeste as a full-size game. It also builds `.p8.png` carts of its own, so the compression formats none of those carts use are covered too. `testdata/README.md` records where each cart came from and under what license.

CI requires 80% statement coverage, which is checked with:

```bash
go test -race -covermode=atomic -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

The code-section reader has a fuzz target, since a cart is only ever partly trustworthy input. Its seed corpus runs as part of `go test`; to fuzz for longer:

```bash
go test -run=^$ -fuzz=FuzzExtractCode -fuzztime=60s
```

Carts placed in `games/` are gitignored, so a local cart collection is never committed:

```bash
go run . games/YourCart.p8.png
```

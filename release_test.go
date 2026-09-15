package main

import (
	"crypto/sha256"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Names repeated across the release target table.
const (
	osDarwin  = "darwin"
	osLinux   = "linux"
	osWindows = "windows"
	archAmd64 = "amd64"
	archArm   = "arm"
	archArm64 = "arm64"
)

// releaseVersion is stamped into the binaries this test builds. It is not a
// version the project will ever release, so a stray artifact is recognizable.
const releaseVersion = "9.9.9"

// checksumFile is the name the Makefile gives the checksum listing.
const checksumFile = "checksums.txt"

// releaseTarget is one platform the release is expected to cover.
type releaseTarget struct {
	goos   string
	goarch string
}

// releaseTargets mirrors the per-target rules in the Makefile. A target added
// there and not here (or the reverse) fails TestBuildRelease.
var releaseTargets = []releaseTarget{
	{osDarwin, archAmd64},
	{osDarwin, archArm64},
	{osLinux, archAmd64},
	{osLinux, archArm},
	{osLinux, archArm64},
	{osWindows, archAmd64},
}

// filename is the name the Makefile writes this target's binary under.
func (t releaseTarget) filename() string {
	name := defaultProgName + "-" + t.goos + "-" + t.goarch
	if t.goos == osWindows {
		name += ".exe"
	}
	return name
}

// TestBuildRelease cross-compiles the whole release matrix and checks that
// every binary really is the platform it is named for. It is the test that
// catches the failure the release is most exposed to: a dependency pulling in
// cgo, which would leave the linux binaries dynamically linked and useless on
// the handhelds they are built for.
func TestBuildRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("cross-compiling every release target is slow")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("make is not available: %v", err)
	}

	dist := t.TempDir()
	out, err := exec.Command("make", "build-release", "DIST="+dist, "VERSION="+releaseVersion).CombinedOutput()
	if err != nil {
		t.Fatalf("make build-release: %v\n%s", err, out)
	}

	assertReleaseContents(t, dist)
	assertChecksums(t, dist)

	for _, target := range releaseTargets {
		t.Run(target.goos+"-"+target.goarch, func(t *testing.T) {
			assertMachine(t, target, filepath.Join(dist, target.filename()))
		})
	}

	assertHostBinaryVersion(t, dist)
}

// assertReleaseContents checks that the release is exactly the expected set of
// files: no target missing, and nothing extra that would be published by the
// dist/* glob in the release workflow.
func assertReleaseContents(t *testing.T, dist string) {
	t.Helper()

	entries, err := os.ReadDir(dist)
	if err != nil {
		t.Fatalf("reading %s: %v", dist, err)
	}
	got := make(map[string]bool, len(entries))
	for _, entry := range entries {
		got[entry.Name()] = true
	}

	want := map[string]bool{checksumFile: true}
	for _, target := range releaseTargets {
		want[target.filename()] = true
	}

	for name := range want {
		if !got[name] {
			t.Errorf("missing release file %s", name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected release file %s", name)
		}
	}
}

// assertChecksums recomputes every digest in checksums.txt. sha256sum and
// shasum -a 256 both write "<hex>  <name>", so the line splits on whitespace.
func assertChecksums(t *testing.T, dist string) {
	t.Helper()

	listing, err := os.ReadFile(filepath.Join(dist, checksumFile))
	if err != nil {
		t.Fatalf("reading %s: %v", checksumFile, err)
	}

	lines := strings.Split(strings.TrimSpace(string(listing)), "\n")
	if len(lines) != len(releaseTargets) {
		t.Errorf("%s has %d lines, want %d", checksumFile, len(lines), len(releaseTargets))
	}

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Errorf("unparseable checksum line %q", line)
			continue
		}
		binary, err := os.ReadFile(filepath.Join(dist, fields[1]))
		if err != nil {
			t.Errorf("reading %s: %v", fields[1], err)
			continue
		}
		sum := sha256.Sum256(binary)
		if got := hex.EncodeToString(sum[:]); got != fields[0] {
			t.Errorf("%s: checksum %s, want %s", fields[1], fields[0], got)
		}
	}
}

// assertMachine reads the binary's object header and checks it was built for
// the machine its name claims. The linux binaries are also checked for a
// PT_INTERP program header: a Go binary that needs a dynamic loader has one,
// and a CGO_ENABLED=0 internally linked binary does not.
func assertMachine(t *testing.T, target releaseTarget, path string) {
	t.Helper()

	switch target.goos {
	case osLinux:
		var want elf.Machine
		switch target.goarch {
		case archAmd64:
			want = elf.EM_X86_64
		case archArm:
			want = elf.EM_ARM
		case archArm64:
			want = elf.EM_AARCH64
		default:
			t.Fatalf("no ELF machine known for %s", target.goarch)
		}

		f, err := elf.Open(path)
		if err != nil {
			t.Fatalf("reading ELF %s: %v", path, err)
		}
		defer f.Close() //nolint:errcheck

		if f.Machine != want {
			t.Errorf("machine = %v, want %v", f.Machine, want)
		}
		for _, prog := range f.Progs {
			if prog.Type == elf.PT_INTERP {
				t.Error("binary is dynamically linked: it has a PT_INTERP program header")
			}
		}

	case osDarwin:
		var want macho.Cpu
		switch target.goarch {
		case archAmd64:
			want = macho.CpuAmd64
		case archArm64:
			want = macho.CpuArm64
		default:
			t.Fatalf("no Mach-O cpu known for %s", target.goarch)
		}

		f, err := macho.Open(path)
		if err != nil {
			t.Fatalf("reading Mach-O %s: %v", path, err)
		}
		defer f.Close() //nolint:errcheck

		if f.Cpu != want {
			t.Errorf("cpu = %v, want %v", f.Cpu, want)
		}

	case osWindows:
		f, err := pe.Open(path)
		if err != nil {
			t.Fatalf("reading PE %s: %v", path, err)
		}
		defer f.Close() //nolint:errcheck

		if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
			t.Errorf("machine = %#x, want %#x", f.Machine, pe.IMAGE_FILE_MACHINE_AMD64)
		}

	default:
		t.Fatalf("no object header check for %s: add one rather than leaving the target unverified", target.goos)
	}
}

// assertHostBinaryVersion runs whichever release binary matches the machine the
// test is running on, which is what proves the ldflags stamping took effect
// rather than just that the link succeeded.
func assertHostBinaryVersion(t *testing.T, dist string) {
	t.Helper()

	host := releaseTarget{runtime.GOOS, runtime.GOARCH}
	for _, target := range releaseTargets {
		if target != host {
			continue
		}

		out, err := exec.Command(filepath.Join(dist, target.filename()), "--version").Output()
		if err != nil {
			t.Fatalf("running %s: %v", target.filename(), err)
		}
		want := defaultProgName + " " + releaseVersion + "\n"
		if string(out) != want {
			t.Errorf("--version = %q, want %q", out, want)
		}
		return
	}

	t.Logf("no release target for %s/%s, version stamping not checked", runtime.GOOS, runtime.GOARCH)
}

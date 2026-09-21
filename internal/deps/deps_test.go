package deps

import (
	"slices"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-lab/internal/platform"
)

func TestInstallablePackages(t *testing.T) {
	required := []Dependency{
		{Name: "qemu", InstallPackages: map[string][]string{"apt": {"qemu-system-x86", "qemu-utils"}}},
		{Name: "dnsmasq", InstallPackages: map[string][]string{"apt": {"dnsmasq"}}},
	}
	pkgs, err := InstallablePackages("apt", required)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("unexpected package count: %d", len(pkgs))
	}
}

func TestUninstallablePackages(t *testing.T) {
	required := []Dependency{
		{Name: "qemu", InstallPackages: map[string][]string{"apt": {"qemu-system-x86", "qemu-utils"}}},
		{Name: "dnsmasq", InstallPackages: map[string][]string{"apt": {"dnsmasq"}}},
	}
	pkgs, err := UninstallablePackages("apt", []string{"qemu"}, required)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("unexpected package count: %d", len(pkgs))
	}
}

// setup used to offer the x86 emulator on an arm64 host, which installs fine
// and leaves DetectPresent still looking for qemu-system-aarch64. See
// kairos-io/kairos#4858.
func TestQemuPackagesMatchTheArchitectureBinary(t *testing.T) {
	cases := []struct {
		arch       string
		wantBinary string
		wantPkg    string
		rejectPkg  string
	}{
		{"arm64", "qemu-system-aarch64", "qemu-system-arm", "qemu-system-x86"},
		{"amd64", "qemu-system-x86_64", "qemu-system-x86", "qemu-system-arm"},
	}

	for _, tc := range cases {
		t.Run(tc.arch, func(t *testing.T) {
			dep := qemuDependency(platform.Info{OS: "linux", Arch: tc.arch})
			if dep.Binaries[0] != tc.wantBinary {
				t.Fatalf("got binary %q, want %q", dep.Binaries[0], tc.wantBinary)
			}
			pkgs, err := InstallablePackages("apt", []Dependency{dep})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(pkgs, tc.wantPkg) {
				t.Errorf("apt packages %v are missing %q", pkgs, tc.wantPkg)
			}
			if slices.Contains(pkgs, tc.rejectPkg) {
				t.Errorf("apt packages %v offer %q, which ships the wrong emulator", pkgs, tc.rejectPkg)
			}
		})
	}
}

// Every package manager has to answer for arm64, otherwise
// InstallablePackages errors out and setup cannot install anything at all.
func TestQemuARM64CoversEveryPackageManager(t *testing.T) {
	dep := qemuDependency(platform.Info{OS: "linux", Arch: "arm64"})
	for _, pm := range []string{"apt", "dnf", "yum", "zypper", "pacman", "apk"} {
		pkgs, err := InstallablePackages(pm, []Dependency{dep})
		if err != nil {
			t.Fatalf("%s: %v", pm, err)
		}
		for _, pkg := range pkgs {
			if strings.Contains(pkg, "x86") {
				t.Errorf("%s offers %q on arm64", pm, pkg)
			}
		}
	}
}

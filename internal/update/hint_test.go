package update

import "testing"

func TestUpgradeHint(t *testing.T) {
	has := func(paths ...string) func(string) bool {
		return func(p string) bool {
			for _, q := range paths {
				if p == q {
					return true
				}
			}
			return false
		}
	}
	none := has()
	const home = "/home/ana"
	installer := "curl -fsSL https://packages.chrom.ar/hkr/install.sh | sh"
	tests := []struct {
		name, exe, goos, gopath string
		has                     func(string) bool
		want                    string
	}{
		{"homebrew cask", "/opt/homebrew/Caskroom/hkr/0.1.1/hkr", "darwin", "", none, "brew upgrade hkr"},
		{"homebrew on intel", "/usr/local/Caskroom/hkr/0.1.1/hkr", "darwin", "", none, "brew upgrade hkr"},
		{"linuxbrew", "/home/linuxbrew/.linuxbrew/Caskroom/hkr/0.1.1/hkr", "linux", "", none, "brew upgrade hkr"},
		{"apt", "/usr/bin/hkr", "linux", "", has("/usr/bin/apt-get"), "sudo apt update && sudo apt upgrade hkr"},
		{"dnf", "/usr/bin/hkr", "linux", "", has("/usr/bin/dnf"), "sudo dnf upgrade hkr"},
		{"zypper", "/usr/bin/hkr", "linux", "", has("/usr/bin/zypper"), "sudo zypper update hkr"},
		{"apk or unknown", "/usr/bin/hkr", "linux", "", none, "download it from https://github.com/chrom-ar/hkr-cli/releases"},
		{"go install default", home + "/go/bin/hkr", "linux", "", none, "go install github.com/chrom-ar/hkr-cli/cmd/hkr@latest"},
		{"go install GOPATH", "/opt/go/bin/hkr", "linux", "/opt/go", none, "go install github.com/chrom-ar/hkr-cli/cmd/hkr@latest"},
		{"installer, system", "/usr/local/bin/hkr", "darwin", "", none, installer},
		{"installer, user", home + "/.local/bin/hkr", "linux", "", none, installer},
		{"installer, elsewhere", "/opt/tools/hkr", "linux", "", none, "curl -fsSL https://packages.chrom.ar/hkr/install.sh | HKR_INSTALL_DIR='/opt/tools' sh"},
		{"windows", `C:\tools\hkr.exe`, "windows", "", none, "download it from https://github.com/chrom-ar/hkr-cli/releases"},
		{"unknown path", "", "linux", "", none, "download it from https://github.com/chrom-ar/hkr-cli/releases"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UpgradeHint(Install{Exe: tt.exe, GOOS: tt.goos, Home: home, GOPATH: tt.gopath, Has: tt.has})
			if got != tt.want {
				t.Errorf("UpgradeHint(%q) = %q, want %q", tt.exe, got, tt.want)
			}
		})
	}
}

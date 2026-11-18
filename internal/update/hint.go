package update

import (
	"path"
	"strings"
)

// Install describes where the running hkr lives, to tell how it
// was installed.
type Install struct {
	Exe    string // The executable's path, symlinks resolved; "" if unknown.
	GOOS   string
	Home   string // The user's home directory.
	GOPATH string // $GOPATH, if set.
	GOBIN  string // $GOBIN, if set.
	// Has reports whether a file exists; it tells the package managers apart.
	Has func(string) bool
}

const (
	releases  = "download it from https://github.com/chrom-ar/hkr-cli/releases"
	installer = "curl -fsSL https://packages.chrom.ar/hkr/install.sh | sh"
)

// UpgradeHint is the command that upgrades hkr the way it was
// installed: Homebrew, a Linux package, go install or the install script.
func UpgradeHint(in Install) string {
	if in.Exe == "" || in.GOOS == "windows" {
		return releases
	}
	dir := path.Dir(in.Exe)
	switch {
	case strings.Contains(in.Exe, "/Caskroom/") || strings.Contains(in.Exe, "/Cellar/") ||
		strings.Contains(in.Exe, "/.linuxbrew/"):
		return "brew upgrade hkr"
	case dir == "/usr/bin":
		switch {
		case in.Has("/usr/bin/apt-get"):
			return "sudo apt update && sudo apt upgrade hkr"
		case in.Has("/usr/bin/dnf"):
			return "sudo dnf upgrade hkr"
		case in.Has("/usr/bin/zypper"):
			return "sudo zypper update hkr"
		}
		return releases
	case isGoBin(dir, in):
		return "go install github.com/chrom-ar/hkr-cli/cmd/hkr@latest"
	case dir == "/usr/local/bin" || dir == path.Join(in.Home, ".local/bin"):
		return installer
	}
	return "curl -fsSL https://packages.chrom.ar/hkr/install.sh | HKR_INSTALL_DIR='" +
		strings.ReplaceAll(dir, "'", `'\''`) + "' sh"
}

func isGoBin(dir string, in Install) bool {
	if in.GOBIN != "" {
		return dir == in.GOBIN
	}
	if in.GOPATH != "" {
		return dir == path.Join(in.GOPATH, "bin")
	}
	return in.Home != "" && dir == path.Join(in.Home, "go/bin")
}

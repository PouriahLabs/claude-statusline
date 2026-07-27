// Package fontinstall downloads and installs a Nerd Font for the current user.
//
// Installing the font is the only part of "make the icons work" that can be
// fully automated. Selecting it in the terminal is a separate step (see
// package terminal), and verifying that the glyphs actually render can only be
// done by asking the user -- there is no portable way to probe glyph coverage.
package fontinstall

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// Pinned rather than "latest" so an upstream retag can't silently change
	// which glyphs exist at which codepoints.
	NerdFontsRelease = "v3.4.0"
	DefaultArchive   = "CascadiaCode"
	DefaultFamily    = "CaskaydiaCove NF"
)

// Font describes an installable choice offered by the wizard.
type Font struct {
	Archive string // release asset name, without .zip
	Family  string // family name to put in the terminal config
	Label   string
}

var Catalog = []Font{
	{"CascadiaCode", "CaskaydiaCove NF", "Cascadia Code (Microsoft; matches Windows Terminal's default)"},
	{"JetBrainsMono", "JetBrainsMono Nerd Font", "JetBrains Mono"},
	{"FiraCode", "FiraCode Nerd Font", "Fira Code"},
	{"Hack", "Hack Nerd Font", "Hack"},
	{"Meslo", "MesloLGS Nerd Font", "Meslo LG"},
}

func Lookup(archive string) (Font, bool) {
	for _, f := range Catalog {
		if strings.EqualFold(f.Archive, archive) {
			return f, true
		}
	}
	return Font{}, false
}

// wantedStyle keeps the install small. A Nerd Font zip carries dozens of
// files (Mono and Propo variants at every weight); a terminal needs four.
func wantedStyle(name string) bool {
	if !strings.HasSuffix(strings.ToLower(name), ".ttf") {
		return false
	}
	base := filepath.Base(name)
	// Skip the Mono/Propo spacing variants -- they use different advance
	// widths and make powerline caps sit unevenly.
	if strings.Contains(base, "NerdFontMono") || strings.Contains(base, "NerdFontPropo") {
		return false
	}
	for _, s := range []string{"-Regular.ttf", "-Bold.ttf", "-Italic.ttf", "-BoldItalic.ttf"} {
		if strings.HasSuffix(base, s) {
			return true
		}
	}
	return false
}

// Install downloads the archive and installs its core styles for the current
// user (never system-wide, so no elevation is required). It returns the
// installed file paths.
func Install(f Font, progress func(string)) ([]string, error) {
	url := fmt.Sprintf("https://github.com/ryanoasis/nerd-fonts/releases/download/%s/%s.zip",
		NerdFontsRelease, f.Archive)
	if progress != nil {
		progress("downloading " + url)
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d for %s", resp.StatusCode, url)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}

	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("unzip: %w", err)
	}

	dir, err := userFontDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}

	var installed []string
	for _, zf := range zr.File {
		if !wantedStyle(zf.Name) {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return installed, err
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return installed, err
		}
		dst := filepath.Join(dir, filepath.Base(zf.Name))
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return installed, fmt.Errorf("write %s: %w", dst, err)
		}
		installed = append(installed, dst)
		if progress != nil {
			progress("installed " + filepath.Base(dst))
		}
	}
	if len(installed) == 0 {
		return nil, fmt.Errorf("archive %s contained no matching .ttf files", f.Archive)
	}
	if err := register(installed, progress); err != nil {
		return installed, err
	}
	return installed, nil
}

func userFontDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Windows", "Fonts"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Fonts"), nil
	default:
		return filepath.Join(home, ".local", "share", "fonts"), nil
	}
}

// register performs the per-OS step that makes a copied file visible as an
// installed font. macOS needs nothing.
func register(paths []string, progress func(string)) error {
	switch runtime.GOOS {
	case "windows":
		// Per-user fonts must be declared under HKCU or apps won't enumerate
		// them. Shelling out to reg.exe avoids a golang.org/x/sys dependency.
		const key = `HKCU\Software\Microsoft\Windows NT\CurrentVersion\Fonts`
		for _, p := range paths {
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)) + " (TrueType)"
			cmd := exec.Command("reg", "add", key, "/v", name, "/t", "REG_SZ", "/d", p, "/f")
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("register %s: %w: %s", name, err, strings.TrimSpace(string(out)))
			}
		}
		if progress != nil {
			progress("registered fonts for the current user")
		}
	case "darwin":
		// Nothing to do; ~/Library/Fonts is scanned automatically.
	default:
		if _, err := exec.LookPath("fc-cache"); err == nil {
			if out, err := exec.Command("fc-cache", "-f").CombinedOutput(); err != nil {
				return fmt.Errorf("fc-cache: %w: %s", err, strings.TrimSpace(string(out)))
			}
			if progress != nil {
				progress("rebuilt the fontconfig cache")
			}
		} else if progress != nil {
			progress("fc-cache not found; you may need to refresh the font cache manually")
		}
	}
	return nil
}

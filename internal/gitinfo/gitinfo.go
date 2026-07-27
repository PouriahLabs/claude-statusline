// Package gitinfo collects branch/dirty/ahead-behind state.
//
// The status line repaints far more often than a repo changes, and shelling
// out to git three times was measured at ~85ms per refresh on a real repo.
// Results are therefore cached in a short-lived temp file keyed by repo path.
package gitinfo

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	Branch string `json:"branch"`
	Dirty  bool   `json:"dirty"`
	Ahead  int    `json:"ahead"`
	Behind int    `json:"behind"`
	IsRepo bool   `json:"is_repo"`

	Stamp int64 `json:"stamp"`
}

// Get returns git state for dir, using a cached value if it is younger than
// ttl. A zero ttl disables caching.
func Get(dir string, ttl time.Duration) Info {
	if dir == "" {
		return Info{}
	}
	if ttl > 0 {
		if c, ok := readCache(dir, ttl); ok {
			return c
		}
	}
	info := collect(dir)
	if ttl > 0 {
		writeCache(dir, info)
	}
	return info
}

func collect(dir string) Info {
	var i Info

	// 'branch --show-current' works in a repo with no commits yet;
	// 'rev-parse --abbrev-ref HEAD' exits 128 there and would hide the pill.
	// It returns empty (exit 0) on a detached HEAD, hence the sha fallback.
	branch, err := run(dir, "branch", "--show-current")
	if err != nil {
		return i
	}
	i.IsRepo = true
	i.Branch = branch
	if i.Branch == "" {
		if sha, err := run(dir, "rev-parse", "--short", "HEAD"); err == nil {
			i.Branch = sha
		}
	}
	if out, err := run(dir, "status", "--porcelain"); err == nil && out != "" {
		i.Dirty = true
	}
	if out, err := run(dir, "rev-list", "--left-right", "--count", "@{upstream}...HEAD"); err == nil {
		// left = upstream-only = behind, right = HEAD-only = ahead
		if f := strings.Fields(out); len(f) == 2 {
			i.Behind, _ = strconv.Atoi(f[0])
			i.Ahead, _ = strconv.Atoi(f[1])
		}
	}
	return i
}

func run(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// CacheDir is the per-user directory holding the git cache files, or "" if the
// platform doesn't report one. Exported so `uninstall` can clear it.
func CacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "claude-statusline")
}

// cachePath keys the cache by repo path, inside a per-user cache directory.
// Deliberately not os.TempDir(): on a shared Linux box /tmp is world-writable
// and the file name is derived from a predictable path, so another local user
// could pre-create it and choose what your status line displays.
func cachePath(dir string) string {
	sum := sha1.Sum([]byte(dir))
	name := hex.EncodeToString(sum[:8]) + ".json"

	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "claude-statusline-git-"+name)
	}
	base = filepath.Join(base, "claude-statusline")
	if os.MkdirAll(base, 0o700) != nil {
		return filepath.Join(os.TempDir(), "claude-statusline-git-"+name)
	}
	return filepath.Join(base, name)
}

func readCache(dir string, ttl time.Duration) (Info, bool) {
	b, err := os.ReadFile(cachePath(dir))
	if err != nil {
		return Info{}, false
	}
	var i Info
	if json.Unmarshal(b, &i) != nil {
		return Info{}, false
	}
	if time.Since(time.Unix(0, i.Stamp)) > ttl {
		return Info{}, false
	}
	return i, true
}

func writeCache(dir string, i Info) {
	i.Stamp = time.Now().UnixNano()
	b, err := json.Marshal(i)
	if err != nil {
		return
	}
	// Write-then-rename so a concurrent reader never sees a partial file. The
	// pid keeps two repaints racing each other from clobbering one temp file.
	path := cachePath(dir)
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if os.WriteFile(tmp, b, 0o600) == nil {
		if os.Rename(tmp, path) != nil {
			_ = os.Remove(tmp)
		}
	}
}

#!/usr/bin/env sh
# claude-statusline installer (macOS / Linux)
#
#   curl -fsSL https://raw.githubusercontent.com/PouriahLabs/claude-statusline/main/install.sh | sh
#
# Downloads the latest release for this platform, installs to ~/.local/bin,
# then hands over to the interactive wizard, which is where the font and
# terminal decisions get made -- nothing is changed without asking.

set -eu

REPO="PouriahLabs/claude-statusline"
BIN="claude-statusline"
PREFIX="${PREFIX:-$HOME/.local/bin}"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux)  os=linux ;;
    darwin) os=macos ;;
    *)      die "unsupported OS: $os (build from source: go install github.com/$REPO@latest)" ;;
esac

arch=$(uname -m)
case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $arch" ;;
esac

command -v curl >/dev/null 2>&1 || die "curl is required"

say "Finding the latest release..."
tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
      | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
[ -n "$tag" ] || die "could not determine the latest release"
version=${tag#v}

asset="${BIN}_${version}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$tag/$asset"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading $asset..."
curl -fsSL "$url" -o "$tmp/pkg.tar.gz" || die "download failed: $url"

# Verify against the published checksums. macOS ships shasum, not sha256sum.
if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
    sha256() { :; }
fi

if curl -fsSL "https://github.com/$REPO/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt" 2>/dev/null; then
    want=$(grep " $asset\$" "$tmp/checksums.txt" | awk '{print $1}')
    got=$(sha256 "$tmp/pkg.tar.gz")
    if [ -n "$want" ] && [ -n "$got" ]; then
        [ "$want" = "$got" ] || die "checksum mismatch for $asset"
        say "Checksum OK."
    else
        say "NOTE: could not verify the checksum (no sha256 tool found)."
    fi
fi

tar -xzf "$tmp/pkg.tar.gz" -C "$tmp"
mkdir -p "$PREFIX"
install -m 0755 "$tmp/$BIN" "$PREFIX/$BIN" 2>/dev/null || {
    cp "$tmp/$BIN" "$PREFIX/$BIN" && chmod 0755 "$PREFIX/$BIN"
}
say "Installed $PREFIX/$BIN"

case ":$PATH:" in
    *":$PREFIX:"*) ;;
    *) say ""; say "NOTE: $PREFIX is not on your PATH. Add this to your shell profile:"
       say "      export PATH=\"\$PATH:$PREFIX\"" ;;
esac

say ""

# Under `curl ... | sh` this script IS stdin, and it is already consumed, so the
# wizard would read EOF and silently take every default. Reattach the terminal
# when there is one; otherwise say what to run by hand.
if [ -r /dev/tty ]; then
    "$PREFIX/$BIN" init < /dev/tty
else
    say "Not running interactively. Finish setup with:"
    say "  $PREFIX/$BIN init"
fi

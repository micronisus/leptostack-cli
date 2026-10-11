#!/bin/sh
# install.sh installs the leptostack binary from the latest GitHub release.
#
#   curl -fsSL https://raw.githubusercontent.com/micronisus/leptostack-cli/main/install.sh | sh
#
# It downloads the platform archive, verifies it against checksums.txt, and
# installs the binary to /usr/local/bin (using sudo when needed).
set -eu

REPO="micronisus/leptostack-cli"
INSTALL_DIR="${LEPTOSTACK_INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
    linux|darwin) ;;
    *) echo "Unsupported OS: $os" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) echo "Unsupported architecture: $arch" >&2; exit 1 ;;
esac

asset="leptostack_${os}_${arch}.tar.gz"
base="https://github.com/${REPO}/releases/latest/download"

tmp=$(mktemp -d)
staged="$INSTALL_DIR/.leptostack.$$"
cleanup() { rm -rf "$tmp"; rm -f "$staged" 2>/dev/null || true; }
trap cleanup EXIT

# Run a command directly when the install dir is writable, else via sudo.
run() {
    if [ -w "$INSTALL_DIR" ] || [ "$(id -u)" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

echo "Downloading ${asset}..."
curl -fsSL -o "$tmp/$asset" "$base/$asset"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

echo "Verifying checksum..."
expected=$(grep " ${asset}\$" "$tmp/checksums.txt" | awk '{print $1}')
if [ -z "$expected" ]; then
    echo "No checksum entry for ${asset}" >&2
    exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$tmp/$asset" | awk '{print $1}')
else
    actual=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
fi
if [ "$expected" != "$actual" ]; then
    echo "Checksum mismatch for ${asset}" >&2
    exit 1
fi

tar -C "$tmp" -xzf "$tmp/$asset"

echo "Installing leptostack to ${INSTALL_DIR}..."
# Stage into the destination directory, then rename atomically so an
# interrupted install never leaves a truncated binary in place.
run install -m 0755 "$tmp/leptostack" "$staged"
run mv -f "$staged" "$INSTALL_DIR/leptostack"

"$INSTALL_DIR/leptostack" version
echo "Installed leptostack. Get started with: leptostack config init && leptostack start"

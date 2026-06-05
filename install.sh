#!/bin/sh
# envault installer for macOS and Linux.
#   curl -fsSL https://raw.githubusercontent.com/fmilioni/envault/main/install.sh | sh
#
# Env vars:
#   ENVAULT_VERSION   install a specific tag (default: latest release)
#   ENVAULT_BASE_URL  override the download base (testing; bypasses VERSION)
set -eu

REPO="fmilioni/envault"
BINARY="envault"

err() { echo "envault-install: $*" >&2; exit 1; }

os=$(uname -s)
case "$os" in
	Darwin) os=darwin ;;
	Linux) os=linux ;;
	*) err "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) err "unsupported architecture: $arch" ;;
esac

asset="${BINARY}_${os}_${arch}"

if [ -n "${ENVAULT_BASE_URL:-}" ]; then
	base="$ENVAULT_BASE_URL"
elif [ -n "${ENVAULT_VERSION:-}" ]; then
	base="https://github.com/${REPO}/releases/download/${ENVAULT_VERSION}"
else
	base="https://github.com/${REPO}/releases/latest/download"
fi

command -v curl >/dev/null 2>&1 || err "curl is required"
command -v sha256sum >/dev/null 2>&1 || command -v shasum >/dev/null 2>&1 || err "sha256sum or shasum is required"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${asset} from ${base} ..."
curl -fsSL "${base}/${asset}" -o "${tmp}/${asset}" || err "download failed: ${base}/${asset}"
curl -fsSL "${base}/checksums.txt" -o "${tmp}/checksums.txt" || err "could not fetch checksums.txt"

expected=$(awk -v a="$asset" '$2 == a {print $1}' "${tmp}/checksums.txt")
[ -n "$expected" ] || err "no checksum entry for ${asset}"
if command -v sha256sum >/dev/null 2>&1; then
	actual=$(sha256sum "${tmp}/${asset}" | awk '{print $1}')
else
	actual=$(shasum -a 256 "${tmp}/${asset}" | awk '{print $1}')
fi
[ "$expected" = "$actual" ] || err "checksum mismatch — corrupt download"

# /usr/local/bin when writable, else a per-user dir we create.
if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
	dir=/usr/local/bin
else
	dir="${HOME}/.local/bin"
	mkdir -p "$dir"
fi

target="${dir}/${BINARY}"
chmod +x "${tmp}/${asset}"
mv -f "${tmp}/${asset}" "$target"
echo "Installed ${BINARY} to ${target}"
"$target" --version 2>/dev/null || true

case ":${PATH}:" in
	*":${dir}:"*) ;;
	*)
		echo ""
		echo "warning: ${dir} is not on your PATH. Add this to your shell profile:"
		echo "  export PATH=\"${dir}:\$PATH\""
		;;
esac

#!/usr/bin/env bash
set -euo pipefail

if [[ -f /etc/os-release ]]; then
	. /etc/os-release
	if [[ "${ID:-}" == "nixos" || "${NAME:-}" == "NixOS" ]]; then
		if command -v nix >/dev/null 2>&1; then
			echo "NixOS detected; installing via nix profile."
			nix profile add github:Axenide/axctl
			exit 0
		fi
		echo "NixOS detected but nix command is unavailable."
		exit 1
	fi
fi

if [[ -f /etc/NIXOS ]]; then
	if command -v nix >/dev/null 2>&1; then
		echo "NixOS detected; installing via nix profile."
		nix profile add github:Axenide/axctl
		exit 0
	fi
	echo "NixOS detected but nix command is unavailable."
	exit 1
fi

os="$(uname -s)"
arch="$(uname -m)"

if [[ "$os" != "Linux" ]]; then
	echo "Unsupported OS: $os"
	exit 1
fi

case "$arch" in
x86_64)
	asset="axctl_linux_amd64"
	;;
i386 | i686)
	asset="axctl_linux_386"
	;;
aarch64)
	asset="axctl_linux_arm64"
	;;
armv7l | armv7 | armv6l)
	asset="axctl_linux_armv7"
	;;
*)
	echo "Unsupported architecture: $arch"
	exit 1
	;;
esac

release_api="https://api.github.com/repos/Axenide/axctl/releases/latest"
latest_tag="$(curl -fsL "$release_api" | grep -E '"tag_name"\s*:' | head -n1 | sed -E 's/.*"tag_name"\s*:\s*"([^"]+)".*/\1/')"
if [[ -z "$latest_tag" ]]; then
	echo "Unable to determine latest release tag."
	exit 1
fi

normalize_version() {
	echo "$1" | sed -E 's/^v//' | grep -Eo '[0-9]+(\.[0-9]+)*' | head -n1 || true
}

latest_version="$(normalize_version "$latest_tag")"
if [[ -z "$latest_version" ]]; then
	echo "Unable to parse latest version from tag: $latest_tag"
	exit 1
fi

if command -v axctl >/dev/null 2>&1; then
	current_raw="$(axctl --version 2>/dev/null || true)"
	current_version="$(normalize_version "$current_raw")"
	if [[ -n "$current_version" ]]; then
		if [[ "$(printf '%s\n%s\n' "$current_version" "$latest_version" | sort -V | head -n1)" == "$latest_version" ]]; then
			echo "already up to date ($current_version)"
			exit 0
		fi
	fi
fi

url="https://github.com/Axenide/axctl/releases/download/${latest_tag}/${asset}"
tmp="$(mktemp)"

cleanup() {
	rm -f "$tmp"
}
trap cleanup EXIT

echo "Downloading $asset (${latest_tag})..."
curl -fL "$url" -o "$tmp"

if [[ $EUID -ne 0 ]]; then
	if command -v sudo >/dev/null 2>&1; then
		sudo install -m 755 "$tmp" /usr/local/bin/axctl
	else
		echo "sudo is required to install to /usr/local/bin."
		exit 1
	fi
else
	install -m 755 "$tmp" /usr/local/bin/axctl
fi
echo "Installed /usr/local/bin/axctl"

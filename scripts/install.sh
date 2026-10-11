#!/bin/sh
# Installs or updates solix-mqtt-bridge as a systemd service.
# Usage: curl -fsSL https://github.com/ryckakas/solix-mqtt-bridge/releases/latest/download/install.sh | sudo sh -s -- [version]
set -eu

name=solix-mqtt-bridge
releases_url=${SOLIX_RELEASES_URL:-https://github.com/ryckakas/solix-mqtt-bridge/releases}
bin=/usr/local/bin/$name
unit=/etc/systemd/system/$name.service
config=/etc/$name.env
script_url=$releases_url/latest/download/install.sh

if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
	bold=$(printf '\033[1m') green=$(printf '\033[32m') yellow=$(printf '\033[33m') red=$(printf '\033[31m')
	reset=$(printf '\033[0m')
else
	bold='' green='' yellow='' red='' reset=''
fi

step() {
	printf '%s==>%s %s\n' "$bold" "$reset" "$*"
}

warn() {
	printf '%s!!%s %s\n' "$yellow" "$reset" "$*"
}

fail() {
	printf '%serror:%s %s\n' "$red" "$reset" "$*" >&2
	exit 1
}

step "$name installer"

[ "$(id -u)" -eq 0 ] || fail "run the installer as root, for example: curl -fsSL $script_url | sudo sh"
[ "$(uname -s)" = Linux ] || fail "this installer sets up a systemd service and needs Linux"
machine=$(uname -m)
case $machine in
aarch64 | arm64) arch=linux_arm64 ;;
# armv8l is a 32-bit kernel on a 64-bit CPU, which can't run arm64 binaries.
armv7l | armv8l) arch=linux_armv7 ;;
x86_64 | amd64) arch=linux_amd64 ;;
*) fail "$machine is not supported. Release builds exist for aarch64 (arm64), armv7l (32-bit ARM) and x86_64." ;;
esac
[ -d /run/systemd/system ] || fail "systemd is not running; this installer sets up a systemd service"
missing=''
for tool in curl tar sha256sum install cmp mktemp systemctl journalctl; do
	command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
done
[ -z "$missing" ] || fail "missing tools:$missing"
step "Checking the system: $arch, systemd, running as root"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

version=${1:-}
version=${version#v}
if [ -z "$version" ]; then
	# The latest-release page redirects to its tag; reading the redirect needs no GitHub API.
	tag_url=$(curl -fsS -o /dev/null -w '%{redirect_url}' "$releases_url/latest") ||
		fail "cannot reach $releases_url/latest"
	case $tag_url in
	*/releases/tag/v*) version=${tag_url##*/v} ;;
	*) fail "could not find the latest release at $releases_url/latest" ;;
	esac
	found="latest is $version"
else
	found=$version
fi
case $version in
*[!0-9A-Za-z.+-]*) fail "invalid version: $version" ;;
esac

download() {
	curl -fsSL -o "$tmp/$1" "$releases_url/download/v$version/$1"
}

sums=${name}_${version}_checksums.txt
archive=${name}_${version}_$arch.tar.gz
download "$sums" 2>/dev/null || fail "release v$version not found at $releases_url"
step "Finding the release: $found"

step "Downloading $archive"
download "$archive" || fail "could not download $archive"

expected=$(awk -v file="$archive" '$2 == file { print $1 }' "$tmp/$sums")
[ -n "$expected" ] || fail "$archive is not listed in $sums; nothing was installed"
actual=$(sha256sum "$tmp/$archive" | cut -d ' ' -f 1)
[ "$actual" = "$expected" ] || fail "checksum mismatch for $archive; nothing was installed"
step "Verifying the checksum: ${green}OK${reset}"

new_unit=deploy/examples/$name.service
template=deploy/examples/$name.env
tar -xzf "$tmp/$archive" -C "$tmp" "$name" "$new_unit" "$template" ||
	fail "$archive lacks the binary or the example files; nothing was installed"

old_version=''
if [ -x "$bin" ]; then
	old_version=$("$bin" -version 2>/dev/null | awk '{ print $2 }') || old_version=''
fi
if [ -e "$config" ]; then first_install=false; else first_install=true; fi

if ! $first_install && cmp -s "$tmp/$name" "$bin" && cmp -s "$tmp/$new_unit" "$unit"; then
	step "Already up to date ($version): nothing installed, the service was left alone"
	printf 'Done: %s %s is installed.\n' "$name" "$version"
	exit 0
fi

if [ -n "$old_version" ]; then
	step "Installing the binary: $bin ($old_version -> $version)"
else
	step "Installing the binary: $bin ($version)"
fi
install -m 0755 "$tmp/$name" "$bin"

reload=true
if [ ! -e "$unit" ]; then
	install -m 0644 "$tmp/$new_unit" "$unit"
	step "Service file: installed $unit"
elif cmp -s "$tmp/$new_unit" "$unit"; then
	reload=false
	step "Service file: unchanged"
else
	install -m 0644 "$tmp/$new_unit" "$unit"
	step "Service file: replaced (it differed from the release's copy)"
	warn "Local changes to the service belong in a drop-in: sudo systemctl edit $name"
fi
if $reload; then systemctl daemon-reload; fi

if $first_install; then
	install -m 0600 "$tmp/$template" "$config"
	step "Configuration: created $config from the template"
	printf 'Done: %s %s is installed but not started yet. Next steps:\n' "$name" "$version"
	printf '  1. Fill in the configuration:  sudo nano %s\n' "$config"
	printf '  2. Check the Solarbank:        SOLARBANK_ADDR=<solarbank-ip>:502 %s -probe\n' "$name"
	printf '  3. Start it, also at boot:     sudo systemctl enable --now %s\n' "$name"
	printf '  4. Follow the log:             journalctl -u %s -f\n' "$name"
	exit 0
fi
step "Configuration: $config kept as it is"

# An enabled unit is meant to run, so a failed one is restarted too: the update may be its fix.
if ! systemctl is-enabled --quiet "$name" && ! systemctl is-active --quiet "$name"; then
	step "Service: not enabled, left stopped"
	printf 'Done: %s %s is installed. Start it, also at boot, with: sudo systemctl enable --now %s\n' \
		"$name" "$version" "$name"
	exit 0
fi

step "Restarting $name"
systemctl restart "$name" || true
printf '%s==>%s Waiting for the service: ' "$bold" "$reset"
sleep 5
state=$(systemctl is-active "$name" || true)
if [ "$state" = active ]; then
	printf '%srunning%s\n' "$green" "$reset"
	printf 'Done: %s %s is running. Follow the log with: journalctl -u %s -f\n' "$name" "$version" "$name"
	exit 0
fi

printf '%s%s%s\n' "$red" "$state" "$reset"
printf '%serror:%s %s %s is not running (%s). Last log lines:\n' "$red" "$reset" "$name" "$version" "$state" >&2
journalctl -u "$name" -n 20 --no-pager >&2 || true
if [ -n "$old_version" ]; then
	printf 'To go back to %s: curl -fsSL %s | sudo sh -s -- %s\n' "$old_version" "$script_url" "$old_version" >&2
fi
exit 1

#!/usr/bin/env bash
#
# SwitchMTP — a macOS MTP client for Nintendo Switch running DBI.
# Copyright (C) 2025 fratei
#
# This program is free software; you can redistribute it and/or modify it
# under the terms of the GNU General Public License version 2 as published by
# the Free Software Foundation.
#
# This program is distributed in the hope that it will be useful, but WITHOUT
# ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
# FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
# more details.
#
# Builds a .deb containing the switchmtp command line tool and the udev rule it
# needs. Must be run on a Debian-derived system: it needs dpkg-deb, and the Go
# build is cgo against libusb so it cannot be cross-compiled from macOS without
# a cross toolchain.
#
# A package is the right shape for this on Linux for one specific reason: it can
# install the udev rule, which a Flatpak or an AppImage cannot. That rule is the
# difference between the tool working and the desktop claiming the console
# first, so anything that cannot ship it leaves the user with a manual step.
#
# Usage:
#   scripts/build-deb.sh                 # build for the host architecture
#   scripts/build-deb.sh --arch arm64    # label an existing cross-built binary

set -euo pipefail

# mktemp -d always creates 0700 and a login shell may set a restrictive umask,
# but dpkg-deb requires the control directory to be group- and world-readable
# and the installed files need to be readable by everyone.
umask 022

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${repo_root}"

arch=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --arch) arch="$2"; shift 2 ;;
        -h|--help) sed -n '18,28p' "${BASH_SOURCE[0]}"; exit 0 ;;
        *) echo "unknown argument: $1" >&2; exit 2 ;;
    esac
done

for tool in dpkg-deb go; do
    if ! command -v "${tool}" >/dev/null 2>&1; then
        echo "error: ${tool} is required and was not found." >&2
        echo "This script builds a Debian package and must run on a Debian-derived system." >&2
        exit 1
    fi
done

if [[ -z "${arch}" ]]; then
    arch="$(dpkg --print-architecture)"
fi

# Debian versions may not contain a leading "v", and must start with a digit.
raw_version="$(git describe --tags --always --dirty 2>/dev/null || echo 0.0.0)"
version="${raw_version#v}"
if [[ ! "${version}" =~ ^[0-9] ]]; then
    version="0.0.0+${version}"
fi

stage="$(mktemp -d)"
trap 'rm -rf "${stage}"' EXIT
chmod 0755 "${stage}"

echo "Building switchmtp ${version} (${arch})"

mkdir -p "${stage}/usr/bin" \
         "${stage}/usr/lib/udev/rules.d" \
         "${stage}/usr/share/doc/switchmtp" \
         "${stage}/DEBIAN"

(
    cd backend
    CGO_ENABLED=1 go build -trimpath \
        -ldflags "-s -w -X main.version=${raw_version}" \
        -o "${stage}/usr/bin/switchmtp" ./cmd/switchmtp
)

# Vendor directory, not /etc: /etc/udev/rules.d is reserved for rules the local
# administrator has written or overridden, and dropping a package file there
# means their edits are silently replaced on upgrade. udev merges both
# directories and sorts by filename across them, so 69-switchmtp.rules still
# sorts after 69-libmtp.rules and still gets the last word on ID_MTP_DEVICE.
install -m 0644 packaging/linux/69-switchmtp.rules \
    "${stage}/usr/lib/udev/rules.d/69-switchmtp.rules"

install -m 0644 LICENSE "${stage}/usr/share/doc/switchmtp/copyright"
install -m 0644 packaging/linux/README.md "${stage}/usr/share/doc/switchmtp/README.md"

installed_size="$(du -ks "${stage}" | cut -f1)"

cat > "${stage}/DEBIAN/control" <<EOF
Package: switchmtp
Version: ${version}
Section: utils
Priority: optional
Architecture: ${arch}
Depends: libusb-1.0-0
Installed-Size: ${installed_size}
Maintainer: fratei <fratei@users.noreply.github.com>
Homepage: https://github.com/fratei/SwitchMTP
Description: Command-line MTP client for a Nintendo Switch running DBI
 Browses a Nintendo Switch running DBI's MTP responder and installs NSP, NSZ,
 XCI and XCZ files to it, one after another from a queue.
 .
 Your desktop can already browse the console over MTP without this package. What
 it cannot do is install titles reliably, tell you which storages only accept
 game files, or queue several installs so DBI is asked for one at a time. Note
 that the two cannot be used at once: after switchmtp has opened the console, a
 file manager's cached MTP mount goes stale and needs remounting.
 .
 Installs a udev rule granting the logged-in user access to the console and
 preventing the desktop's own MTP support from claiming it first.
EOF

cat > "${stage}/DEBIAN/postinst" <<'EOF'
#!/bin/sh
set -e

# The rule only takes effect for devices that enumerate after it is loaded, so
# reload and retrigger rather than telling the user to replug the console.
if [ "$1" = "configure" ]; then
    if command -v udevadm >/dev/null 2>&1; then
        udevadm control --reload-rules >/dev/null 2>&1 || true
        udevadm trigger --subsystem-match=usb >/dev/null 2>&1 || true
    fi
fi

exit 0
EOF

cat > "${stage}/DEBIAN/postrm" <<'EOF'
#!/bin/sh
set -e

# Removing the rule hands the console back to the desktop's own MTP support, but
# only once udev has been told the rule is gone.
if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
    if command -v udevadm >/dev/null 2>&1; then
        udevadm control --reload-rules >/dev/null 2>&1 || true
        udevadm trigger --subsystem-match=usb >/dev/null 2>&1 || true
    fi
fi

exit 0
EOF

chmod 0755 "${stage}/DEBIAN/postinst" "${stage}/DEBIAN/postrm"

mkdir -p build
output="build/switchmtp_${version}_${arch}.deb"
dpkg-deb --root-owner-group --build "${stage}" "${output}" >/dev/null

echo "Wrote ${output}"

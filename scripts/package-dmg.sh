#!/usr/bin/env bash
#
# Packages build/Release/SwitchMTP.app into a compressed DMG and verifies it.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_ROOT="${ROOT}/build"
APP="${1:-${BUILD_ROOT}/Release/SwitchMTP.app}"
PLIST="${APP}/Contents/Info.plist"
DMG_ROOT="${BUILD_ROOT}/dmg-root"
MOUNT="${BUILD_ROOT}/dmg-mount"

info() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v hdiutil >/dev/null 2>&1 || die "hdiutil is not installed."
command -v codesign >/dev/null 2>&1 || die "codesign is not installed."
[[ -d "${APP}" ]] || die "app bundle not found: ${APP}. Run scripts/build-app.sh first."
[[ -f "${PLIST}" ]] || die "Info.plist not found: ${PLIST}"

version="$(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "${PLIST}")"
[[ -n "${version}" ]] || die "CFBundleShortVersionString is empty in ${PLIST}"
DMG="${BUILD_ROOT}/SwitchMTP-${version}-universal.dmg"

info "Preparing DMG contents"
rm -rf "${DMG_ROOT}" "${MOUNT}" "${DMG}" "${DMG}.sha256"
mkdir -p "${DMG_ROOT}" "${MOUNT}"
cp -R "${APP}" "${DMG_ROOT}/SwitchMTP.app"
ln -s /Applications "${DMG_ROOT}/Applications"

info "Creating ${DMG}"
hdiutil create \
  -volname "SwitchMTP ${version}" \
  -srcfolder "${DMG_ROOT}" \
  -ov \
  -format UDZO \
  "${DMG}"

info "Writing checksum"
shasum -a 256 "${DMG}" > "${DMG}.sha256"
cat "${DMG}.sha256"

attached=0
cleanup() {
  if (( attached )); then
    hdiutil detach "${MOUNT}" -quiet || true
  fi
}
trap cleanup EXIT

info "Mounting DMG for verification"
hdiutil attach "${DMG}" -mountpoint "${MOUNT}" -nobrowse -quiet
attached=1

[[ -d "${MOUNT}/SwitchMTP.app" ]] || die "SwitchMTP.app is missing from mounted DMG"
info "Verifying app signature inside DMG"
codesign --verify --deep --strict --verbose=2 "${MOUNT}/SwitchMTP.app"

info "Unmounting DMG"
hdiutil detach "${MOUNT}" -quiet
attached=0

info "Done: ${DMG}"

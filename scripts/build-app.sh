#!/usr/bin/env bash
#
# Builds a Release SwitchMTP.app bundle into build/ and verifies its embedded dylibs.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APP_DIR="${ROOT}/app"
BUILD_ROOT="${ROOT}/build"
PRODUCTS="${BUILD_ROOT}/Release"
APP="${PRODUCTS}/SwitchMTP.app"
APP_BINARY="${APP}/Contents/MacOS/SwitchMTP"
FRAMEWORKS="${APP}/Contents/Frameworks"
ENTITLEMENTS="${APP_DIR}/Resources/SwitchMTP.entitlements"

info() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v xcodebuild >/dev/null 2>&1 || die "xcodebuild is not installed. See scripts/bootstrap.sh."
command -v codesign >/dev/null 2>&1 || die "codesign is not installed. See scripts/bootstrap.sh."
command -v lipo >/dev/null 2>&1 || die "lipo is not installed. See scripts/bootstrap.sh."
command -v otool >/dev/null 2>&1 || die "otool is not installed. See scripts/bootstrap.sh."

if [[ -z "${DEVELOPER_DIR:-}" ]]; then
  selected_developer_dir="$(xcode-select -p 2>/dev/null || true)"
  if [[ "${selected_developer_dir}" == *CommandLineTools* && -d /Applications/Xcode.app/Contents/Developer ]]; then
    export DEVELOPER_DIR="/Applications/Xcode.app/Contents/Developer"
  elif [[ -n "${selected_developer_dir}" ]]; then
    export DEVELOPER_DIR="${selected_developer_dir}"
  fi
fi

info "Building backend"
"${ROOT}/scripts/build-backend.sh"

if command -v xcodegen >/dev/null 2>&1; then
  info "Generating Xcode project"
  (cd "${APP_DIR}" && xcodegen generate)
elif [[ ! -d "${APP_DIR}/SwitchMTP.xcodeproj" ]]; then
  die "XcodeGen is not installed and ${APP_DIR}/SwitchMTP.xcodeproj does not exist. Install with: brew install xcodegen"
fi

rm -rf "${BUILD_ROOT}/DerivedData" "${PRODUCTS}"
mkdir -p "${PRODUCTS}"

info "Building SwitchMTP.app"
xcodebuild \
  -project "${APP_DIR}/SwitchMTP.xcodeproj" \
  -scheme SwitchMTP \
  -configuration Release \
  -destination 'generic/platform=macOS' \
  -derivedDataPath "${BUILD_ROOT}/DerivedData" \
  ARCHS='arm64 x86_64' \
  ONLY_ACTIVE_ARCH=NO \
  CONFIGURATION_BUILD_DIR="${PRODUCTS}" \
  build

[[ -d "${APP}" ]] || die "expected app bundle was not produced: ${APP}"
[[ -x "${APP_BINARY}" ]] || die "expected app binary was not produced: ${APP_BINARY}"

info "Embedding backend dylibs"
mkdir -p "${FRAMEWORKS}"
cp "${ROOT}/backend/build/nxmtp.dylib" "${FRAMEWORKS}/nxmtp.dylib"
cp "${ROOT}/backend/build/libusb-1.0.0.dylib" "${FRAMEWORKS}/libusb-1.0.0.dylib"

chmod u+w "${APP_BINARY}" "${FRAMEWORKS}/nxmtp.dylib" "${FRAMEWORKS}/libusb-1.0.0.dylib"
install_name_tool -id "@rpath/nxmtp.dylib" "${FRAMEWORKS}/nxmtp.dylib"
install_name_tool -id "@loader_path/libusb-1.0.0.dylib" "${FRAMEWORKS}/libusb-1.0.0.dylib"
if otool -L "${FRAMEWORKS}/nxmtp.dylib" | grep -q "@rpath/libusb-1.0.0.dylib"; then
  install_name_tool -change "@rpath/libusb-1.0.0.dylib" "@loader_path/libusb-1.0.0.dylib" "${FRAMEWORKS}/nxmtp.dylib"
fi
if otool -L "${APP_BINARY}" | grep -q "${ROOT}/backend/build/nxmtp.dylib"; then
  install_name_tool -change "${ROOT}/backend/build/nxmtp.dylib" "@rpath/nxmtp.dylib" "${APP_BINARY}"
fi
if otool -L "${APP_BINARY}" | grep -q "${ROOT}/third_party/libusb/lib/libusb-1.0.0.dylib"; then
  install_name_tool -change "${ROOT}/third_party/libusb/lib/libusb-1.0.0.dylib" "@rpath/libusb-1.0.0.dylib" "${APP_BINARY}"
fi
if otool -L "${APP_BINARY}" | grep -q "@loader_path/libusb-1.0.0.dylib"; then
  install_name_tool -change "@loader_path/libusb-1.0.0.dylib" "@rpath/libusb-1.0.0.dylib" "${APP_BINARY}"
fi

info "Building switchmtp-cli"
xcodebuild \
  -project "${APP_DIR}/SwitchMTP.xcodeproj" \
  -scheme switchmtp-cli \
  -configuration Release \
  -destination 'generic/platform=macOS' \
  -derivedDataPath "${BUILD_ROOT}/DerivedData" \
  ARCHS='arm64 x86_64' \
  ONLY_ACTIVE_ARCH=NO \
  CONFIGURATION_BUILD_DIR="${PRODUCTS}" \
  build

CLI_SRC="${PRODUCTS}/switchmtp-cli"
[[ -x "${CLI_SRC}" ]] || die "expected CLI binary was not produced: ${CLI_SRC}"

info "Embedding switchmtp-cli"
CLI="${APP}/Contents/MacOS/switchmtp-cli"
cp "${CLI_SRC}" "${CLI}"
chmod u+w "${CLI}"
# The CLI links the dylibs by absolute build path; inside the bundle they live in
# ../Frameworks, which @rpath + LD_RUNPATH_SEARCH_PATHS resolves.
for dylib in nxmtp libusb-1.0.0; do
  for prefix in "${ROOT}/backend/build" "${ROOT}/third_party/libusb/lib"; do
    if otool -L "${CLI}" | grep -q "${prefix}/${dylib}.dylib"; then
      install_name_tool -change "${prefix}/${dylib}.dylib" "@rpath/${dylib}.dylib" "${CLI}"
    fi
  done
done
if otool -L "${CLI}" | grep -q "@loader_path/libusb-1.0.0.dylib"; then
  install_name_tool -change "@loader_path/libusb-1.0.0.dylib" "@rpath/libusb-1.0.0.dylib" "${CLI}"
fi
# Drop any absolute build-tree rpaths so the shipped binary cannot resolve
# against this machine.
while read -r rp; do
  case "${rp}" in
    @*) ;;
    *) install_name_tool -delete_rpath "${rp}" "${CLI}" 2>/dev/null || true ;;
  esac
done < <(otool -l "${CLI}" | awk '/LC_RPATH/{f=1} f&&/ path /{print $2; f=0}')
# install_name_tool invalidates the signature, and --deep does not reliably
# cover a second executable in Contents/MacOS -- sign it explicitly.
codesign --force --sign - --timestamp=none --options runtime \
  --entitlements "${APP_DIR}/Resources/switchmtp-cli.entitlements" "${CLI}"
lipo -info "${CLI}" | grep -q 'arm64' || die "switchmtp-cli is missing arm64"
lipo -info "${CLI}" | grep -q 'x86_64' || die "switchmtp-cli is missing x86_64"
otool -L "${CLI}"

info "Signing app"
# Sign inside-out. --deep is avoided because it would re-sign switchmtp-cli
# with the app's sandbox entitlements, which a CLI cannot use.
codesign --force --sign - --timestamp=none "${FRAMEWORKS}/libusb-1.0.0.dylib"
codesign --force --sign - --timestamp=none "${FRAMEWORKS}/nxmtp.dylib"
codesign --force --sign - --timestamp=none --entitlements "${ENTITLEMENTS}" "${APP}"

info "Verifying embedded dylibs"
[[ -f "${FRAMEWORKS}/nxmtp.dylib" ]] || die "missing ${FRAMEWORKS}/nxmtp.dylib; nxmtp.dylib and libusb must sit side by side"
[[ -f "${FRAMEWORKS}/libusb-1.0.0.dylib" ]] || die "missing ${FRAMEWORKS}/libusb-1.0.0.dylib; nxmtp.dylib and libusb must sit side by side"

info "App binary architectures"
lipo -info "${APP_BINARY}"
lipo -info "${APP_BINARY}" | grep -q 'arm64' || die "SwitchMTP binary is missing arm64"
lipo -info "${APP_BINARY}" | grep -q 'x86_64' || die "SwitchMTP binary is missing x86_64"

info "App binary linked libraries"
otool -L "${APP_BINARY}"
info "nxmtp.dylib linked libraries"
otool -L "${FRAMEWORKS}/nxmtp.dylib"
info "libusb-1.0.0.dylib linked libraries"
otool -L "${FRAMEWORKS}/libusb-1.0.0.dylib"

info "Frameworks contents"
find "${FRAMEWORKS}" -maxdepth 1 -type f -print | sort

info "Verifying bundled CLI runs"
"${APP}/Contents/MacOS/switchmtp-cli" devices >/dev/null || die "bundled switchmtp-cli failed to run"

info "Verifying code signature"
codesign --verify --deep --strict --verbose=2 "${APP}"

info "Done: ${APP}"

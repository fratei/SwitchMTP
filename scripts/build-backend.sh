#!/usr/bin/env bash
#
# Builds nxmtp.dylib -- the Go backend -- as a universal (arm64 + x86_64)
# c-shared library, together with the generated C header and a copy of libusb.
#
# The three files land in backend/build/ and are what the Xcode project embeds
# into SwitchMTP.app/Contents/Frameworks.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BACKEND="${ROOT}/backend"
BUILD="${BACKEND}/build"
LIBUSB="${ROOT}/third_party/libusb"

info() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v go >/dev/null 2>&1 || die "go is not installed. See scripts/bootstrap.sh."

# libusb must exist before the backend can link. Building it here rather than
# failing keeps a fresh clone to a single command.
if [[ ! -f "${LIBUSB}/lib/libusb-1.0.0.dylib" ]]; then
  info "libusb not found, building it first"
  "${ROOT}/scripts/build-libusb.sh"
fi

# cgo needs an SDK path; the Command Line Tools alone do not always export one.
export SDKROOT="${SDKROOT:-$(xcrun --sdk macosx --show-sdk-path)}"
export CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"

rm -rf "${BUILD}"
mkdir -p "${BUILD}/arm64" "${BUILD}/x86_64"

build_arch() {
  local goarch="$1" clangarch="$2" out="$3"
  info "Building nxmtp.dylib for ${clangarch}"
  (
    cd "${BACKEND}"
    GOARCH="${goarch}" GOOS=darwin \
      CC="clang -arch ${clangarch} -mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET}" \
      CXX="clang++ -arch ${clangarch} -mmacosx-version-min=${MACOSX_DEPLOYMENT_TARGET}" \
      go build -trimpath -buildmode=c-shared -o "${out}/nxmtp.dylib" ./ffi
  )
}

build_arch arm64 arm64 "${BUILD}/arm64"
build_arch amd64 x86_64 "${BUILD}/x86_64"

info "Creating universal binary"
lipo -create \
  "${BUILD}/arm64/nxmtp.dylib" \
  "${BUILD}/x86_64/nxmtp.dylib" \
  -output "${BUILD}/nxmtp.dylib"

# The header is architecture-independent; either copy will do.
cp "${BUILD}/arm64/nxmtp.h" "${BUILD}/nxmtp.h"

# @rpath lets the app bundle resolve the dylib from Contents/Frameworks
# without the build machine's absolute paths leaking into the binary.
install_name_tool -id "@rpath/nxmtp.dylib" "${BUILD}/nxmtp.dylib"

# libusb is referenced as @rpath/libusb-1.0.0.dylib. Adding @loader_path
# resolves that to whatever directory nxmtp.dylib is copied into, which is what
# makes the app bundle work.
install_name_tool -add_rpath "@loader_path" "${BUILD}/nxmtp.dylib"

# third_party/usb bakes in an absolute rpath so `go test` binaries, which run
# from temp directories, can find libusb. That path is meaningless on a user's
# machine and would leak this build tree into a shipped artefact, so drop it.
for rp in $(otool -l "${BUILD}/nxmtp.dylib" \
            | awk '/LC_RPATH/{f=1} f&&/path /{print $2; f=0}' \
            | grep -v '^@' | sort -u); do
  install_name_tool -delete_rpath "${rp}" "${BUILD}/nxmtp.dylib" 2>/dev/null || true
done

# nxmtp.dylib resolves libusb through @loader_path, so the two must sit side by
# side wherever they are copied.
cp "${LIBUSB}/lib/libusb-1.0.0.dylib" "${BUILD}/libusb-1.0.0.dylib"
chmod u+w "${BUILD}/libusb-1.0.0.dylib"

# Re-sign after install_name_tool: editing a Mach-O invalidates its signature,
# and macOS refuses to load an unsigned-but-modified dylib.
codesign --force --sign - --timestamp=none "${BUILD}/nxmtp.dylib"
codesign --force --sign - --timestamp=none "${BUILD}/libusb-1.0.0.dylib"

rm -rf "${BUILD}/arm64" "${BUILD}/x86_64"

info "Verifying"
lipo -info "${BUILD}/nxmtp.dylib"
otool -L "${BUILD}/nxmtp.dylib" | sed -n '2,$p'

missing=()
for sym in FetchAvailableDevices Initialize FetchDeviceInfo FetchStorages Walk \
           MakeDirectory RenameFile DeleteFile FileExists DownloadFiles \
           UploadFiles CancelTransfer Dispose FetchDiagnostics; do
  nm -gU "${BUILD}/nxmtp.dylib" | grep -q "_${sym}\$" || missing+=("${sym}")
done
if (( ${#missing[@]} )); then
  die "missing exports: ${missing[*]}"
fi
info "All ${#missing[@]} expected exports present ($(nm -gU "${BUILD}/nxmtp.dylib" | wc -l | tr -d ' ') symbols total)"
info "Done: ${BUILD}/nxmtp.dylib"

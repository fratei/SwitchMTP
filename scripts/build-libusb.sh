#!/usr/bin/env bash
#
# Builds a universal (arm64 + x86_64) libusb into third_party/libusb/.
#
# Why we build it ourselves instead of using Homebrew:
#   * Homebrew's libusb is built for the host's current macOS version, which produces
#     "built for newer macOS version than being linked" warnings and can break the
#     10-year-old deployment target we ship.
#   * Homebrew is not architecture-universal, so a Mac on Apple Silicon cannot produce
#     an Intel-compatible build.
#   * It makes the build hermetic: no pkg-config, no brew, reproducible in CI.
#
# The resulting dylib carries an install_name of @rpath/libusb-1.0.0.dylib so it
# can simply sit next to nxmtp.dylib inside the app bundle's Frameworks directory.

set -euo pipefail

LIBUSB_VERSION="${LIBUSB_VERSION:-1.0.27}"
LIBUSB_SHA256="ffaa41d741a8a3bee244ac8e54a72ea05bf2879663c098c82fc5757853441575"
DEPLOYMENT_TARGET="${MACOSX_DEPLOYMENT_TARGET:-12.0}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/third_party/libusb"
WORK="$ROOT/.build/libusb"
INSTALL_NAME="@rpath/libusb-1.0.0.dylib"

if [[ "${1:-}" == "--force" ]]; then
  rm -rf "$OUT" "$WORK"
fi

if [[ -f "$OUT/lib/libusb-1.0.dylib" && -f "$OUT/include/libusb-1.0/libusb.h" ]]; then
  echo "libusb already built at $OUT (use --force to rebuild)"
  lipo -info "$OUT/lib/libusb-1.0.0.dylib"
  exit 0
fi

mkdir -p "$WORK"
cd "$WORK"

TARBALL="libusb-${LIBUSB_VERSION}.tar.bz2"
URL="https://github.com/libusb/libusb/releases/download/v${LIBUSB_VERSION}/${TARBALL}"

if [[ ! -f "$TARBALL" ]]; then
  echo "==> Downloading $URL"
  curl -fsSL --retry 3 -o "$TARBALL" "$URL"
fi

echo "==> Verifying checksum"
echo "${LIBUSB_SHA256}  ${TARBALL}" | shasum -a 256 -c -

rm -rf "libusb-${LIBUSB_VERSION}"
tar xjf "$TARBALL"
SRC="$WORK/libusb-${LIBUSB_VERSION}"

build_arch() {
  local arch="$1"
  local prefix="$WORK/install-$arch"

  # A configure run leaves state behind, so each architecture gets a clean tree.
  local tree="$WORK/build-$arch"
  rm -rf "$tree" "$prefix"
  cp -R "$SRC" "$tree"
  cd "$tree"

  echo "==> Configuring libusb for $arch"
  # --disable-udev: udev is Linux-only; on macOS libusb uses IOKit regardless, but the
  # configure script can still get confused if it finds a stray libudev.
  CFLAGS="-arch $arch -mmacosx-version-min=$DEPLOYMENT_TARGET -O2" \
  LDFLAGS="-arch $arch -mmacosx-version-min=$DEPLOYMENT_TARGET" \
    ./configure \
      --prefix="$prefix" \
      --host="${arch}-apple-darwin" \
      --disable-dependency-tracking \
      --disable-udev \
      --enable-shared \
      --disable-static \
      >"$WORK/configure-$arch.log" 2>&1 \
    || { echo "configure failed for $arch; tail of log:"; tail -40 "$WORK/configure-$arch.log"; exit 1; }

  echo "==> Building libusb for $arch"
  make -j"$(sysctl -n hw.ncpu)" >"$WORK/make-$arch.log" 2>&1 \
    || { echo "make failed for $arch; tail of log:"; tail -40 "$WORK/make-$arch.log"; exit 1; }
  make install >>"$WORK/make-$arch.log" 2>&1
}

build_arch arm64
build_arch x86_64

echo "==> Creating universal binary"
rm -rf "$OUT"
mkdir -p "$OUT/lib" "$OUT/include"
cp -R "$WORK/install-arm64/include/libusb-1.0" "$OUT/include/"

lipo -create \
  "$WORK/install-arm64/lib/libusb-1.0.0.dylib" \
  "$WORK/install-x86_64/lib/libusb-1.0.0.dylib" \
  -output "$OUT/lib/libusb-1.0.0.dylib"

# The linker resolves -lusb-1.0 through this symlink; the app bundle ships the real file.
ln -sf libusb-1.0.0.dylib "$OUT/lib/libusb-1.0.dylib"

install_name_tool -id "$INSTALL_NAME" "$OUT/lib/libusb-1.0.0.dylib"
codesign --force --sign - "$OUT/lib/libusb-1.0.0.dylib" 2>/dev/null || true

cat > "$OUT/VERSION" <<EOF
libusb $LIBUSB_VERSION
Built by scripts/build-libusb.sh for macOS $DEPLOYMENT_TARGET+ (arm64 + x86_64).
Licensed under LGPL-2.1-or-later; see LICENSE. Linked dynamically by nxmtp.dylib.
EOF
cp "$SRC/COPYING" "$OUT/LICENSE"

echo "==> Done"
lipo -info "$OUT/lib/libusb-1.0.0.dylib"
otool -D "$OUT/lib/libusb-1.0.0.dylib"

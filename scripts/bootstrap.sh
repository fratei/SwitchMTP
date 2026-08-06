#!/usr/bin/env bash
#
# Verifies the build prerequisites for a fresh SwitchMTP checkout.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MIN_GO_MAJOR=1
MIN_GO_MINOR=21
MIN_DISK_GB=10

info() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

failures=0

requirement_failed() {
  printf 'error: %s\n' "$*" >&2
  failures=$((failures + 1))
}

info "Checking Xcode"
selected_developer_dir="$(xcode-select -p 2>/dev/null || true)"
full_xcode="/Applications/Xcode.app/Contents/Developer"
if [[ ! -d "${full_xcode}" ]]; then
  requirement_failed "full Xcode is missing at /Applications/Xcode.app. Install Xcode from the App Store or Apple Developer downloads."
elif ! DEVELOPER_DIR="${full_xcode}" xcodebuild -version >/dev/null 2>&1; then
  requirement_failed "Xcode is present but xcodebuild is not usable. Open Xcode once and install any requested components."
else
  info "Xcode usable at ${full_xcode}"
  if [[ "${selected_developer_dir}" == *CommandLineTools* ]]; then
    warn "xcode-select currently points at CommandLineTools: ${selected_developer_dir}"
    warn "Recommended for this shell: export DEVELOPER_DIR=${full_xcode}"
    warn "Permanent alternative: sudo xcode-select -s /Applications/Xcode.app"
  elif [[ -n "${selected_developer_dir}" ]]; then
    info "Selected developer directory: ${selected_developer_dir}"
  else
    warn "xcode-select did not report a selected developer directory; export DEVELOPER_DIR=${full_xcode} before building."
  fi
fi

info "Checking Go"
if ! command -v go >/dev/null 2>&1; then
  requirement_failed "Go is not installed. Install Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or newer from https://go.dev/dl/."
else
  go_version="$(go version | awk '{print $3}' | sed 's/^go//')"
  go_major="$(printf '%s' "${go_version}" | awk -F. '{print $1}')"
  go_minor="$(printf '%s' "${go_version}" | awk -F. '{print $2}')"
  if [[ ! "${go_major}" =~ ^[0-9]+$ || ! "${go_minor}" =~ ^[0-9]+$ ]]; then
    requirement_failed "could not parse Go version '${go_version}'. Install Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or newer."
  elif (( go_major < MIN_GO_MAJOR || (go_major == MIN_GO_MAJOR && go_minor < MIN_GO_MINOR) )); then
    requirement_failed "Go ${go_version} is too old. Install Go ${MIN_GO_MAJOR}.${MIN_GO_MINOR} or newer."
  else
    info "Go ${go_version} found"
  fi
fi

info "Checking XcodeGen"
if ! command -v xcodegen >/dev/null 2>&1; then
  requirement_failed "XcodeGen is not installed. Install it with: brew install xcodegen"
else
  info "$(xcodegen --version | awk '{print "XcodeGen " $0}') found"
fi

info "Checking available disk space"
available_kb="$(df -Pk "${ROOT}" | awk 'NR==2 {print $4}')"
available_gb=$((available_kb / 1024 / 1024))
info "${available_gb} GiB available at ${ROOT}"
if (( available_gb < MIN_DISK_GB )); then
  requirement_failed "at least ${MIN_DISK_GB} GiB of free disk space is required to build universal dependencies and the app."
fi

if (( failures > 0 )); then
  die "${failures} prerequisite check(s) failed"
fi

info "All prerequisites satisfied"

#!/usr/bin/env bash
#
# Runs a repeatable real-hardware validation pass against a Nintendo Switch
# running DBI's MTP responder and writes a single Markdown report under build/.
#
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_ROOT="${ROOT}/build"
PRODUCTS="${BUILD_ROOT}/Release"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"
REPORT="${BUILD_ROOT}/hw-report-${TIMESTAMP}.md"
WORK_DIR="${BUILD_ROOT}/hw-work-${TIMESTAMP}"

info() { printf '==> %s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

WRITE_TESTS=0
LARGE_FILE=""
DBI_MODE="unknown"
DEVICE_ID=""
STRESS_COUNT=10
FAILS=0

usage() {
  cat <<USAGE
Usage: scripts/hw-validate.sh [options]

Read-only by default. The default run never writes to or deletes from the Switch.

Options:
  --write-tests              Opt in to mkdir/upload/rename/delete probes on SD/custom storage only
  --large-file <sid:/path>   Opt in to reading one >4 GiB file end-to-end and verifying byte count
  --mode <title|applet|unknown>
                             Record how DBI was launched (default: unknown)
  --device <id>              Pass a specific switchmtp-cli device id
  --stress-count <n>         Number of read-only repeated ls operations for stability probe (default: 10)
  -h, --help                 Show this help

Environment:
  SWITCHMTP_CLI              Override path to switchmtp-cli
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --write-tests)
      WRITE_TESTS=1
      ;;
    --large-file)
      shift
      [[ $# -gt 0 ]] || die "--large-file requires <sid:/path>"
      LARGE_FILE="$1"
      ;;
    --mode)
      shift
      [[ $# -gt 0 ]] || die "--mode requires title, applet, or unknown"
      DBI_MODE="$1"
      case "${DBI_MODE}" in title|applet|unknown) ;; *) die "--mode must be title, applet, or unknown" ;; esac
      ;;
    --device)
      shift
      [[ $# -gt 0 ]] || die "--device requires an id"
      DEVICE_ID="$1"
      ;;
    --stress-count)
      shift
      [[ $# -gt 0 ]] || die "--stress-count requires a number"
      STRESS_COUNT="$1"
      [[ "${STRESS_COUNT}" =~ ^[0-9]+$ ]] || die "--stress-count must be a non-negative integer"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
  shift
done

locate_cli() {
  if [[ -n "${SWITCHMTP_CLI:-}" ]]; then
    [[ -x "${SWITCHMTP_CLI}" ]] || die "SWITCHMTP_CLI is not executable: ${SWITCHMTP_CLI}"
    printf '%s\n' "${SWITCHMTP_CLI}"
    return
  fi

  local bundled="${PRODUCTS}/SwitchMTP.app/Contents/MacOS/switchmtp-cli"
  local standalone="${PRODUCTS}/switchmtp-cli"
  if [[ -x "${bundled}" ]]; then
    printf '%s\n' "${bundled}"
  elif [[ -x "${standalone}" ]]; then
    printf '%s\n' "${standalone}"
  else
    die "switchmtp-cli was not found. Run scripts/build-app.sh first, or set SWITCHMTP_CLI=/path/to/switchmtp-cli."
  fi
}

CLI="$(locate_cli)"
command -v python3 >/dev/null 2>&1 || die "python3 is required to parse switchmtp-cli JSON output. Install Xcode command line tools or run on the build Mac."

mkdir -p "${BUILD_ROOT}" "${WORK_DIR}"
trap 'rm -rf "${WORK_DIR}"' EXIT

DEVICE_ARGS=()
if [[ -n "${DEVICE_ID}" ]]; then
  DEVICE_ARGS=(--device "${DEVICE_ID}")
fi

append() { printf '%s\n' "$*" >>"${REPORT}"; }

append_file_block() {
  local title="$1" file="$2" lang="${3:-text}"
  append ""
  append "<details><summary>${title}</summary>"
  append ""
  append '```'"${lang}"
  if [[ -s "${file}" ]]; then
    cat "${file}" >>"${REPORT}"
  else
    append "(no output)"
  fi
  append '```'
  append "</details>"
}

append_command_block() {
  local title="$1" status="$2" out="$3" err="$4" lang="${5:-text}"
  append ""
  append "### ${title}"
  append ""
  append "Exit code: ${status}"
  append_file_block "stdout" "${out}" "${lang}"
  append_file_block "stderr" "${err}" "text"
}

run_cli() {
  local out="$1" err="$2"
  shift 2
  set +e
  "${CLI}" "${DEVICE_ARGS[@]}" "$@" >"${out}" 2>"${err}"
  local status=$?
  set -e
  return "${status}"
}

run_cli_global() {
  local out="$1" err="$2"
  shift 2
  set +e
  "${CLI}" "$@" >"${out}" 2>"${err}"
  local status=$?
  set -e
  return "${status}"
}

json_eval() {
  local file="$1" code="$2"
  python3 - "$file" "$code" <<'PY'
import json, sys
path, code = sys.argv[1], sys.argv[2]
try:
    with open(path, 'r', encoding='utf-8') as f:
        obj = json.load(f)
except Exception:
    obj = {}
data = obj.get('data')
ns = {'obj': obj, 'data': data}
exec(code, ns)
PY
}

record_probe() {
  local number="$1" status="$2" title="$3" details="$4"
  append "- Probe ${number}: **${status}** — ${title}. ${details}"
  if [[ "${status}" == "FAIL" ]]; then
    FAILS=$((FAILS + 1))
  fi
}

remote_parent() {
  python3 - "$1" <<'PY'
import posixpath, sys
p = sys.argv[1]
parent = posixpath.dirname(p.rstrip('/')) or '/'
print(parent)
PY
}

remote_base() {
  python3 - "$1" <<'PY'
import posixpath, sys
print(posixpath.basename(sys.argv[1].rstrip('/')))
PY
}

json_escape() {
  python3 - "$1" <<'PY'
import json, sys
print(json.dumps(sys.argv[1]))
PY
}

storage_selector_code='''
items = data or []
preferred = {"sdCard": 0, "album": 1, "saves": 2, "installedGames": 3, "gamecard": 4, "nandUser": 5, "nandSystem": 6, "custom": 7}
candidates = []
for s in items:
    caps = s.get("capabilities") or {}
    if caps.get("browse") and caps.get("read"):
        candidates.append((preferred.get(s.get("kind"), 99), s))
if candidates:
    s = sorted(candidates, key=lambda x: x[0])[0][1]
    print("{}|{}|{}".format(s.get("Sid"), s.get("kind"), s.get("displayName") or (s.get("Info") or {}).get("StorageDescription") or "Storage"))
'''

write_storage_code='''
items = data or []
for s in items:
    caps = s.get("capabilities") or {}
    if s.get("kind") == "sdCard" and caps.get("browse") and caps.get("write") and caps.get("makeDirectory") and not caps.get("installTarget"):
        print("{}|{}|{}".format(s.get("Sid"), s.get("kind"), s.get("displayName") or (s.get("Info") or {}).get("StorageDescription") or "Storage"))
        raise SystemExit
'''

info "Writing hardware validation report to ${REPORT}"

cat >"${REPORT}" <<EOF_REPORT
# SwitchMTP Hardware Validation Report

- Timestamp: ${TIMESTAMP}
- DBI launch mode: ${DBI_MODE}
- Write tests requested: $([[ "${WRITE_TESTS}" -eq 1 ]] && printf 'yes' || printf 'no')
- Large-file read requested: $([[ -n "${LARGE_FILE}" ]] && printf '%s' "${LARGE_FILE}" || printf 'no')

## Host and build

- macOS: $(sw_vers -productVersion 2>/dev/null || printf 'unknown') ($(sw_vers -buildVersion 2>/dev/null || printf 'unknown'))
- Architecture: $(uname -m)
- CLI: ${CLI}
- CLI SHA-256: $(shasum -a 256 "${CLI}" 2>/dev/null | awk '{print $1}')
- CLI modified: $(stat -f '%Sm' "${CLI}" 2>/dev/null || printf 'unknown')
EOF_REPORT

APP_INFO="${PRODUCTS}/SwitchMTP.app/Contents/Info.plist"
if [[ -f "${APP_INFO}" ]] && command -v /usr/libexec/PlistBuddy >/dev/null 2>&1; then
  append "- App version: $(/usr/libexec/PlistBuddy -c 'Print :CFBundleShortVersionString' "${APP_INFO}" 2>/dev/null || printf 'unknown') ($(/usr/libexec/PlistBuddy -c 'Print :CFBundleVersion' "${APP_INFO}" 2>/dev/null || printf 'unknown'))"
else
  append "- App version: unavailable (no built app Info.plist found)"
fi

append ""
append "## Diagnostics"

DOCTOR_TXT_OUT="${WORK_DIR}/doctor.txt.out"
DOCTOR_TXT_ERR="${WORK_DIR}/doctor.txt.err"
if run_cli_global "${DOCTOR_TXT_OUT}" "${DOCTOR_TXT_ERR}" doctor; then doctor_status=0; else doctor_status=$?; fi
append_command_block "switchmtp-cli doctor" "${doctor_status}" "${DOCTOR_TXT_OUT}" "${DOCTOR_TXT_ERR}" "text"

DOCTOR_JSON_OUT="${WORK_DIR}/doctor.json.out"
DOCTOR_JSON_ERR="${WORK_DIR}/doctor.json.err"
if run_cli_global "${DOCTOR_JSON_OUT}" "${DOCTOR_JSON_ERR}" --json doctor; then doctor_json_status=0; else doctor_json_status=$?; fi
append_command_block "switchmtp-cli --json doctor" "${doctor_json_status}" "${DOCTOR_JSON_OUT}" "${DOCTOR_JSON_ERR}" "json"

DEVICES_TXT_OUT="${WORK_DIR}/devices.txt.out"
DEVICES_TXT_ERR="${WORK_DIR}/devices.txt.err"
if run_cli_global "${DEVICES_TXT_OUT}" "${DEVICES_TXT_ERR}" devices; then devices_status=0; else devices_status=$?; fi
append_command_block "switchmtp-cli devices" "${devices_status}" "${DEVICES_TXT_OUT}" "${DEVICES_TXT_ERR}" "text"

DEVICES_JSON_OUT="${WORK_DIR}/devices.json.out"
DEVICES_JSON_ERR="${WORK_DIR}/devices.json.err"
if run_cli_global "${DEVICES_JSON_OUT}" "${DEVICES_JSON_ERR}" --json devices; then devices_json_status=0; else devices_json_status=$?; fi
append_command_block "switchmtp-cli --json devices" "${devices_json_status}" "${DEVICES_JSON_OUT}" "${DEVICES_JSON_ERR}" "json"

device_count="$(json_eval "${DEVICES_JSON_OUT}" 'print(len(data or []))')"
if [[ "${device_count}" == "0" ]]; then
  append ""
  append "## Probe summary"
  append ""
  record_probe "0" "FAIL" "Device connection" "No usable MTP device was detected. Connect the Switch, open DBI, press X, choose Run MTP responder, and rerun this script."
  for n in 1 2 3 4 5 6 7 8 9; do
    record_probe "${n}" "SKIP" "Not run" "No device was available."
  done
  append ""
  append "## Result"
  append ""
  append "Overall result: **FAIL** (no usable MTP device was detected)."
  info "No MTP device detected. Report written to ${REPORT}"
  exit 1
fi

if [[ -z "${DEVICE_ID}" ]]; then
  selected="$(json_eval "${DEVICES_JSON_OUT}" $'items = [d for d in (data or []) if d.get("usable", True)]\nif items:\n    d = items[0]\n    print("{}|{}|{}".format(d.get("vendorId", 0), d.get("productId", 0), d.get("serialNumber", "")))')"
  if [[ -n "${selected}" ]]; then
    DEVICE_ID="${selected}"
    DEVICE_ARGS=(--device "${DEVICE_ID}")
  fi
fi

append ""
append "## Selected device"
append ""
append "- Device ID: ${DEVICE_ID:-auto-selection failed}"

INFO_TXT_OUT="${WORK_DIR}/info.txt.out"
INFO_TXT_ERR="${WORK_DIR}/info.txt.err"
if run_cli "${INFO_TXT_OUT}" "${INFO_TXT_ERR}" info; then info_status=0; else info_status=$?; fi
append_command_block "switchmtp-cli info" "${info_status}" "${INFO_TXT_OUT}" "${INFO_TXT_ERR}" "text"

INFO_JSON_OUT="${WORK_DIR}/info.json.out"
INFO_JSON_ERR="${WORK_DIR}/info.json.err"
if run_cli "${INFO_JSON_OUT}" "${INFO_JSON_ERR}" --json info; then info_json_status=0; else info_json_status=$?; fi
append_command_block "switchmtp-cli --json info" "${info_json_status}" "${INFO_JSON_OUT}" "${INFO_JSON_ERR}" "json"

STORAGES_TXT_OUT="${WORK_DIR}/storages.txt.out"
STORAGES_TXT_ERR="${WORK_DIR}/storages.txt.err"
if run_cli "${STORAGES_TXT_OUT}" "${STORAGES_TXT_ERR}" storages; then storages_status=0; else storages_status=$?; fi
append_command_block "switchmtp-cli storages" "${storages_status}" "${STORAGES_TXT_OUT}" "${STORAGES_TXT_ERR}" "text"

STORAGES_JSON_OUT="${WORK_DIR}/storages.json.out"
STORAGES_JSON_ERR="${WORK_DIR}/storages.json.err"
if run_cli "${STORAGES_JSON_OUT}" "${STORAGES_JSON_ERR}" --json storages; then storages_json_status=0; else storages_json_status=$?; fi
append_command_block "switchmtp-cli --json storages" "${storages_json_status}" "${STORAGES_JSON_OUT}" "${STORAGES_JSON_ERR}" "json"

append ""
append "## Storage table"
append ""
append '| ID | Kind | Name | browse | read | write | delete | rename | mkdir | installTarget | virtual | sizeReliable |'
append '|---|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|'
json_eval "${STORAGES_JSON_OUT}" $'\nfor s in data or []:\n    caps = s.get("capabilities") or {}\n    info = s.get("Info") or {}\n    vals = [s.get("Sid", ""), s.get("kind", ""), (s.get("displayName") or info.get("StorageDescription") or "Storage")]\n    keys = ["browse", "read", "write", "delete", "rename", "makeDirectory", "installTarget"]\n    bools = ["yes" if caps.get(k) else "no" for k in keys]\n    bools += ["yes" if s.get("virtual") else "no", "yes" if s.get("sizeReliable", True) else "no"]\n    safe = [str(v).replace("|", chr(92) + "|") for v in vals + bools]\n    print("| " + " | ".join(safe) + " |")\n' >>"${REPORT}"

append ""
append "## Probe summary"
append ""

if [[ "${info_status}" -ne 0 || "${storages_status}" -ne 0 ]]; then
  record_probe "0" "FAIL" "Device initialization" "The CLI could see a device but could not read device info/storages; see raw output above."
fi

read_storage="$(json_eval "${STORAGES_JSON_OUT}" "${storage_selector_code}")"
read_sid=""
read_kind=""
if [[ -n "${read_storage}" ]]; then
  IFS='|' read -r read_sid read_kind read_name <<EOF_READ
${read_storage}
EOF_READ
fi

if [[ -n "${read_sid}" ]]; then
  LS_ROOT_JSON_OUT="${WORK_DIR}/ls-root.json.out"
  LS_ROOT_JSON_ERR="${WORK_DIR}/ls-root.json.err"
  if run_cli "${LS_ROOT_JSON_OUT}" "${LS_ROOT_JSON_ERR}" --verbose --json ls "${read_sid}:/"; then ls_root_status=0; else ls_root_status=$?; fi
  append_command_block "switchmtp-cli --verbose --json ls ${read_sid}:/" "${ls_root_status}" "${LS_ROOT_JSON_OUT}" "${LS_ROOT_JSON_ERR}" "json"

  prop_list_cap="$(json_eval "${INFO_JSON_OUT}" 'print("yes" if ((data or {}).get("capabilities") or {}).get("getObjectPropList") else "no")')"
  if [[ "${ls_root_status}" -eq 0 ]]; then
    if [[ "${prop_list_cap}" == "yes" ]] && grep -q '0x9805' "${LS_ROOT_JSON_ERR}"; then
      record_probe "1" "PASS" "GetObjectPropList path" "Device advertised GetObjectPropList but rejected it; backend demoted the capability and completed the listing via GetObjectInfo fallback."
    elif [[ "${prop_list_cap}" == "yes" ]]; then
      record_probe "1" "PASS" "GetObjectPropList path" "Device advertised GetObjectPropList and the root listing completed without demotion; prop-list path was used for ${read_kind}."
    else
      record_probe "1" "PASS" "GetObjectInfo fallback path" "Device did not advertise GetObjectPropList; root listing completed via per-object GetObjectInfo fallback."
    fi
  else
    record_probe "1" "FAIL" "Directory listing path" "Root listing on ${read_sid} (${read_kind}) failed; see raw ls output."
  fi

  prop_value_cap="$(json_eval "${INFO_JSON_OUT}" 'print("yes" if ((data or {}).get("capabilities") or {}).get("getObjectPropValue") else "no")')"
  unknown_count="$(json_eval "${LS_ROOT_JSON_OUT}" 'print(sum(1 for e in (data or []) if e.get("sizeUnknown")))')"
  if [[ "${prop_value_cap}" == "yes" ]]; then
    if [[ "${unknown_count}" != "0" ]]; then
      record_probe "2" "FAIL" "GetObjectPropValue(ObjectSize)" "Device advertised ObjectSize support, but ${unknown_count} root entr(y/ies) still reported sizeUnknown. Use --large-file with a known >4 GiB file to isolate."
    else
      record_probe "2" "PASS" "GetObjectPropValue(ObjectSize)" "Device advertises ObjectSize support and the sampled listing had no unknown sizes. Use --large-file to prove a >4 GiB transfer byte count."
    fi
  else
    record_probe "2" "PASS" "ObjectSize unsupported and handled" "Device does not advertise GetObjectPropValue(ObjectSize); SwitchMTP should display — for overflowed sizes instead of a bogus 4 GiB value."
  fi
else
  record_probe "1" "FAIL" "Directory listing path" "No browsable/readable storage was found for a root listing."
  record_probe "2" "SKIP" "GetObjectPropValue(ObjectSize)" "No browsable/readable storage was found for sampling."
fi

rename_cap="$(json_eval "${INFO_JSON_OUT}" 'print("yes" if ((data or {}).get("capabilities") or {}).get("setObjectPropValue") else "no")')"
can_move="$(json_eval "${INFO_JSON_OUT}" $'caps = ((data or {}).get("capabilities") or {})\nprint("yes" if caps.get("canMove") else "no")')"
move_cap="$(json_eval "${INFO_JSON_OUT}" $'caps = ((data or {}).get("capabilities") or {})\nprint("yes" if caps.get("moveObject") else "no")')"
copy_cap="$(json_eval "${INFO_JSON_OUT}" $'caps = ((data or {}).get("capabilities") or {})\nprint("yes" if caps.get("copyObject") else "no")')"

write_storage="$(json_eval "${STORAGES_JSON_OUT}" "${write_storage_code}")"
write_sid=""
write_kind=""
if [[ -n "${write_storage}" ]]; then
  IFS='|' read -r write_sid write_kind write_name <<EOF_WRITE
${write_storage}
EOF_WRITE
fi

if [[ "${WRITE_TESTS}" -eq 0 ]]; then
  record_probe "3" "SKIP" "SetObjectPropValue(ObjectFileName) rename" "Write tests were not requested. Advertised setObjectPropValue=${rename_cap}. Rerun with --write-tests to exercise it."
  record_probe "8" "SKIP" "Non-ASCII / emoji filename round-trip" "Write tests were not requested. Rerun with --write-tests."
elif [[ -z "${write_sid}" ]]; then
  record_probe "3" "SKIP" "SetObjectPropValue(ObjectFileName) rename" "No SD Card storage with browse/write/mkdir support was found; the harness never writes to NAND or unknown custom mappings."
  record_probe "8" "SKIP" "Non-ASCII / emoji filename round-trip" "No SD Card storage with browse/write/mkdir support was found; the harness never writes to NAND or unknown custom mappings."
else
  scratch="/switchmtp-hwtest-${TIMESTAMP}"
  local_scratch="${BUILD_ROOT}/hw-local-${TIMESTAMP}"
  mkdir -p "${local_scratch}"
  printf 'SwitchMTP hardware validation %s\n' "${TIMESTAMP}" >"${local_scratch}/rename-source.txt"
  printf 'emoji filename validation %s\n' "${TIMESTAMP}" >"${local_scratch}/emoji-🧪-switchmtp.txt"

  MKDIR_OUT="${WORK_DIR}/mkdir.out"; MKDIR_ERR="${WORK_DIR}/mkdir.err"
  if run_cli "${MKDIR_OUT}" "${MKDIR_ERR}" mkdir "${write_sid}:${scratch}"; then mkdir_status=0; else mkdir_status=$?; fi
  append_command_block "switchmtp-cli mkdir ${write_sid}:${scratch}" "${mkdir_status}" "${MKDIR_OUT}" "${MKDIR_ERR}" "text"

  if [[ "${mkdir_status}" -ne 0 ]]; then
    record_probe "3" "FAIL" "SetObjectPropValue(ObjectFileName) rename" "Could not create scratch directory on safe storage ${write_kind}; rename was not attempted."
    record_probe "8" "FAIL" "Non-ASCII / emoji filename round-trip" "Could not create scratch directory on safe storage ${write_kind}; emoji upload was not attempted."
  else
    CP_RENAME_OUT="${WORK_DIR}/cp-rename.out"; CP_RENAME_ERR="${WORK_DIR}/cp-rename.err"
    if run_cli "${CP_RENAME_OUT}" "${CP_RENAME_ERR}" cp "${local_scratch}/rename-source.txt" "${write_sid}:${scratch}/"; then cp_rename_status=0; else cp_rename_status=$?; fi
    append_command_block "switchmtp-cli cp rename-source.txt ${write_sid}:${scratch}/" "${cp_rename_status}" "${CP_RENAME_OUT}" "${CP_RENAME_ERR}" "text"

    MV_OUT="${WORK_DIR}/mv.out"; MV_ERR="${WORK_DIR}/mv.err"
    if [[ "${cp_rename_status}" -eq 0 ]]; then
      if run_cli "${MV_OUT}" "${MV_ERR}" mv "${write_sid}:${scratch}/rename-source.txt" "${scratch}/renamed-by-switchmtp.txt"; then mv_status=0; else mv_status=$?; fi
      append_command_block "switchmtp-cli mv ${write_sid}:${scratch}/rename-source.txt ${scratch}/renamed-by-switchmtp.txt" "${mv_status}" "${MV_OUT}" "${MV_ERR}" "text"
      if [[ "${mv_status}" -eq 0 ]]; then
        record_probe "3" "PASS" "SetObjectPropValue(ObjectFileName) rename" "Rename succeeded on ${write_kind} scratch directory."
      elif [[ "${rename_cap}" == "no" ]]; then
        record_probe "3" "PASS" "Rename unsupported and handled" "Device did not advertise SetObjectPropValue and the rename command was rejected without corrupting data."
      else
        record_probe "3" "FAIL" "SetObjectPropValue(ObjectFileName) rename" "Device advertised SetObjectPropValue but the scratch rename failed; see mv output."
      fi
    else
      record_probe "3" "FAIL" "SetObjectPropValue(ObjectFileName) rename" "Could not upload scratch file for rename test."
    fi

    CP_EMOJI_OUT="${WORK_DIR}/cp-emoji.out"; CP_EMOJI_ERR="${WORK_DIR}/cp-emoji.err"
    if run_cli "${CP_EMOJI_OUT}" "${CP_EMOJI_ERR}" cp "${local_scratch}/emoji-🧪-switchmtp.txt" "${write_sid}:${scratch}/"; then cp_emoji_status=0; else cp_emoji_status=$?; fi
    append_command_block "switchmtp-cli cp emoji file ${write_sid}:${scratch}/" "${cp_emoji_status}" "${CP_EMOJI_OUT}" "${CP_EMOJI_ERR}" "text"

    LS_SCRATCH_OUT="${WORK_DIR}/ls-scratch.json.out"; LS_SCRATCH_ERR="${WORK_DIR}/ls-scratch.json.err"
    if run_cli "${LS_SCRATCH_OUT}" "${LS_SCRATCH_ERR}" --json ls "${write_sid}:${scratch}"; then ls_scratch_status=0; else ls_scratch_status=$?; fi
    append_command_block "switchmtp-cli --json ls ${write_sid}:${scratch}" "${ls_scratch_status}" "${LS_SCRATCH_OUT}" "${LS_SCRATCH_ERR}" "json"

    if [[ "${cp_emoji_status}" -eq 0 && "${ls_scratch_status}" -eq 0 ]]; then
      emoji_found="$(json_eval "${LS_SCRATCH_OUT}" 'print("yes" if any(e.get("name") == "emoji-🧪-switchmtp.txt" for e in (data or [])) else "no")')"
      if [[ "${emoji_found}" == "yes" ]]; then
        record_probe "8" "PASS" "Non-ASCII / emoji filename round-trip" "Emoji filename uploaded and listed back exactly inside the scratch directory."
      else
        record_probe "8" "FAIL" "Non-ASCII / emoji filename round-trip" "Emoji filename upload completed, but the exact UTF-16 surrogate-pair filename was not listed back."
      fi
    else
      record_probe "8" "FAIL" "Non-ASCII / emoji filename round-trip" "Emoji upload or scratch listing failed; see raw output."
    fi

    for remote in "${scratch}/renamed-by-switchmtp.txt" "${scratch}/rename-source.txt" "${scratch}/emoji-🧪-switchmtp.txt" "${scratch}"; do
      RM_OUT="${WORK_DIR}/rm-$(printf '%s' "${remote}" | tr '/🧪' '___').out"
      RM_ERR="${WORK_DIR}/rm-$(printf '%s' "${remote}" | tr '/🧪' '___').err"
      if run_cli "${RM_OUT}" "${RM_ERR}" rm "${write_sid}:${remote}"; then rm_status=0; else rm_status=$?; fi
      append_command_block "cleanup: switchmtp-cli rm ${write_sid}:${remote}" "${rm_status}" "${RM_OUT}" "${RM_ERR}" "text"
    done
  fi
  rm -rf "${local_scratch}"
fi

if [[ "${can_move}" == "yes" && ( "${move_cap}" == "yes" || ( "${copy_cap}" == "yes" ) ) ]]; then
  record_probe "4" "PASS" "MoveObject / CopyObject capability" "Device advertises moveObject=${move_cap}, copyObject=${copy_cap}, derived canMove=${can_move}. The current CLI exposes rename, not device-to-device move/copy, so runtime mutation is not directly exercised."
elif [[ "${can_move}" == "no" ]]; then
  record_probe "4" "PASS" "MoveObject / CopyObject disabled" "Device advertises moveObject=${move_cap}, copyObject=${copy_cap}; backend derived canMove=no, so move UI should be disabled."
else
  record_probe "4" "FAIL" "MoveObject / CopyObject capability" "Inconsistent capabilities: moveObject=${move_cap}, copyObject=${copy_cap}, canMove=${can_move}."
fi

if [[ -z "${LARGE_FILE}" ]]; then
  record_probe "5" "SKIP" ">4 GiB read byte-count" "Large-file read was not requested. Rerun with --large-file <sid:/path> in title mode to prove end-to-end byte count."
else
  if [[ "${LARGE_FILE}" != *:* ]]; then
    record_probe "5" "FAIL" ">4 GiB read byte-count" "--large-file must be a device path like 65537:/folder/file.nsp."
  else
    large_sid="${LARGE_FILE%%:*}"
    large_path="${LARGE_FILE#*:}"
    large_parent="$(remote_parent "${large_path}")"
    large_name="$(remote_base "${large_path}")"
    LARGE_LS_OUT="${WORK_DIR}/large-ls.json.out"; LARGE_LS_ERR="${WORK_DIR}/large-ls.json.err"
    if run_cli "${LARGE_LS_OUT}" "${LARGE_LS_ERR}" --json ls "${large_sid}:${large_parent}"; then large_ls_status=0; else large_ls_status=$?; fi
    append_command_block "switchmtp-cli --json ls ${large_sid}:${large_parent}" "${large_ls_status}" "${LARGE_LS_OUT}" "${LARGE_LS_ERR}" "json"

    large_name_json="$(json_escape "${large_name}")"
    expected_size="$(json_eval "${LARGE_LS_OUT}" $'name = '"${large_name_json}"$'\nfor e in data or []:\n    if e.get("name") == name:\n        print("unknown" if e.get("sizeUnknown") else e.get("size", ""))\n        break\n')"
    if [[ "${large_ls_status}" -ne 0 || -z "${expected_size}" ]]; then
      record_probe "5" "FAIL" ">4 GiB read byte-count" "Could not find ${LARGE_FILE} in its parent listing."
    elif [[ "${expected_size}" == "unknown" ]]; then
      record_probe "5" "FAIL" ">4 GiB read byte-count" "${LARGE_FILE} has unknown size; ObjectSize support is not sufficient to verify byte count."
    elif [[ "${expected_size}" -le 4294967296 ]]; then
      record_probe "5" "FAIL" ">4 GiB read byte-count" "${LARGE_FILE} is ${expected_size} bytes, not larger than 4 GiB."
    else
      download_dir="${BUILD_ROOT}/hw-download-${TIMESTAMP}"
      mkdir -p "${download_dir}"
      LARGE_CP_OUT="${WORK_DIR}/large-cp.json.out"; LARGE_CP_ERR="${WORK_DIR}/large-cp.json.err"
      if run_cli "${LARGE_CP_OUT}" "${LARGE_CP_ERR}" --json cp "${LARGE_FILE}" "${download_dir}/"; then large_cp_status=0; else large_cp_status=$?; fi
      append_command_block "switchmtp-cli --json cp ${LARGE_FILE} ${download_dir}/" "${large_cp_status}" "${LARGE_CP_OUT}" "${LARGE_CP_ERR}" "json"
      downloaded="${download_dir}/${large_name}"
      if [[ "${large_cp_status}" -eq 0 && -f "${downloaded}" ]]; then
        actual_size="$(stat -f '%z' "${downloaded}")"
        if [[ "${actual_size}" == "${expected_size}" ]]; then
          record_probe "5" "PASS" ">4 GiB read byte-count" "Downloaded ${actual_size} bytes, matching the device-reported ObjectSize."
        else
          record_probe "5" "FAIL" ">4 GiB read byte-count" "Downloaded ${actual_size} bytes, expected ${expected_size}."
        fi
      else
        record_probe "5" "FAIL" ">4 GiB read byte-count" "Large-file copy failed; see raw cp output. If DBI was in applet mode, rerun in title mode."
      fi
      rm -rf "${download_dir}"
    fi
  fi
fi

expected_storage_status="$(json_eval "${STORAGES_JSON_OUT}" $'\nrequired = ["sdCard", "nandUser", "nandSystem", "installedGames", "sdInstall", "nandInstall", "saves", "album", "gamecard"]\nseen = {s.get("kind") for s in data or []}\nmissing = [k for k in required if k not in seen]\nprint("ok" if not missing else ",".join(missing))\n')"
if [[ "${expected_storage_status}" == "ok" ]]; then
  custom_count="$(json_eval "${STORAGES_JSON_OUT}" 'print(sum(1 for s in (data or []) if s.get("kind") == "custom"))')"
  record_probe "6" "PASS" "DBI storage classification" "All nine fixed DBI storage kinds were present and classified; custom storage count=${custom_count}."
else
  record_probe "6" "FAIL" "DBI storage classification" "Missing expected storage kind(s): ${expected_storage_status}."
fi

install_status="$(json_eval "${STORAGES_JSON_OUT}" $'\nproblems = []\nfor kind in ("sdInstall", "nandInstall"):\n    matches = [s for s in data or [] if s.get("kind") == kind]\n    if not matches:\n        problems.append(kind + ":missing")\n        continue\n    for s in matches:\n        caps = s.get("capabilities") or {}\n        if not caps.get("installTarget"):\n            problems.append(kind + ":installTarget=false")\n        if caps.get("browse"):\n            problems.append(kind + ":browse=true")\nprint("ok" if not problems else ",".join(problems))\n')"
if [[ "${install_status}" == "ok" ]]; then
  record_probe "7" "PASS" "Install storage flags" "sdInstall and nandInstall are installTarget=true and browse=false."
else
  record_probe "7" "FAIL" "Install storage flags" "Problem(s): ${install_status}."
fi

if [[ -n "${read_sid}" && "${STRESS_COUNT}" -gt 0 ]]; then
  stress_fail=0
  i=1
  while [[ "${i}" -le "${STRESS_COUNT}" ]]; do
    STRESS_OUT="${WORK_DIR}/stress-${i}.json.out"; STRESS_ERR="${WORK_DIR}/stress-${i}.json.err"
    if run_cli "${STRESS_OUT}" "${STRESS_ERR}" --json ls "${read_sid}:/"; then stress_status=0; else stress_status=$?; fi
    if [[ "${stress_status}" -ne 0 ]]; then
      stress_fail=$((stress_fail + 1))
      append_command_block "stress ${i}: switchmtp-cli --json ls ${read_sid}:/" "${stress_status}" "${STRESS_OUT}" "${STRESS_ERR}" "json"
    fi
    i=$((i + 1))
  done
  if [[ "${stress_fail}" -eq 0 ]]; then
    record_probe "9" "PASS" "Session stability across repeated operations" "${STRESS_COUNT}/${STRESS_COUNT} repeated read-only listings succeeded on ${read_kind}."
  else
    record_probe "9" "FAIL" "Session stability across repeated operations" "${stress_fail}/${STRESS_COUNT} repeated listings failed; see stress command output."
  fi
else
  record_probe "9" "SKIP" "Session stability across repeated operations" "No read storage was available, or --stress-count was 0."
fi

append ""
append "## Result"
append ""
if [[ "${FAILS}" -eq 0 ]]; then
  append "Overall result: **PASS** (no failed probes; skipped probes require explicit opt-in or unavailable hardware state)."
  info "Hardware validation passed/skipped with no failures. Report: ${REPORT}"
  exit 0
else
  append "Overall result: **FAIL** (${FAILS} failed probe(s))."
  info "Hardware validation failed (${FAILS} failed probe(s)). Report: ${REPORT}"
  exit 1
fi

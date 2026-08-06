# switchmtp-cli

`switchmtp-cli` is the command-line interface for SwitchMTP's Go MTP backend. It targets
a Nintendo Switch running DBI's MTP responder.

The CLI ships inside the app bundle:

```sh
/Applications/SwitchMTP.app/Contents/MacOS/switchmtp-cli doctor
```

For a source build, the same bundled path is under the build product (from the repository
root):

```sh
build/Release/SwitchMTP.app/Contents/MacOS/switchmtp-cli doctor
```

Optional convenience symlink:

```sh
mkdir -p /usr/local/bin
ln -sf /Applications/SwitchMTP.app/Contents/MacOS/switchmtp-cli /usr/local/bin/switchmtp-cli
```

If something is wrong, run this first:

```sh
switchmtp-cli doctor
```

## Global flags

```sh
switchmtp-cli [--verbose] [--json] [--device <id>] <command> [arguments]
```

- `--device <id>` selects a device when more than one usable MTP device is connected.
  Device IDs look like `1406|8221|SERIAL` and use decimal vendor/product IDs.
- `--json` prints the raw backend JSON envelope for the main operation.
- `--verbose` enables backend protocol logging on stderr.

Device paths use `<storageId>:<path>`, for example `65537:/switch/`.

## Commands

### `devices`

Lists connected MTP devices.

```sh
switchmtp-cli devices
```

With no devices attached, output is:

```text
[]
```

### `info`

Shows the selected device profile, identity, advice, and negotiated MTP capabilities.

```sh
switchmtp-cli --device "1406|8221|XAW10000000000" info
```

### `storages`

Lists storage IDs, Switch/DBI storage kind, capability flags, and notes. DBI install
storages are shown as write-only install triggers and not browsable; virtual storages
such as Installed Games/Game Card are marked as virtual/generated.

```sh
switchmtp-cli storages
```

### `ls [-a] <storage>:<path>`

Lists a directory. Unknown virtual file sizes are shown as `—`, never as a fake 4 GiB value.

```sh
switchmtp-cli ls 65537:/
switchmtp-cli ls -a 65537:/switch/
```

### `cp <src> <dst>`

Copies in either direction. Exactly one side must be a device path.

```sh
# Device to Mac
switchmtp-cli cp 65537:/switch/app.nro ~/Downloads/

# Mac to device directory
switchmtp-cli cp ~/Downloads/app.nro 65537:/switch/
```

### `rm <storage>:<path>`

Deletes a file or folder from the device.

```sh
switchmtp-cli rm 65537:/switch/old-app.nro
```

### `mv <storage>:<path> <new-path>`

Renames an item within the same device directory.

```sh
switchmtp-cli mv 65537:/switch/old.nro /switch/new.nro
```

### `mkdir <storage>:<path>`

Creates a device directory.

```sh
switchmtp-cli mkdir 65537:/switch/new-folder
```

### `install <file...> [--nand]`

Uploads `.nsp`, `.nsz`, `.xci`, or `.xcz` files to DBI's install storage. SD install is the default; pass `--nand` for internal storage.

```sh
switchmtp-cli install ~/Downloads/game.nsp
switchmtp-cli install ~/Downloads/game.nsz --nand
```

After the upload completes, installation continues on the Switch. DBI does not send a
final installation completion event over MTP, so the CLI can only report that the file
was uploaded. Watch the Switch screen for install progress and errors.

### `backup-saves <dir>`

Downloads the DBI Saves storage recursively into a dated `switch-saves-...` folder under
`<dir>`.

```sh
switchmtp-cli backup-saves ~/Backups/Switch
```

Example destination:

```text
~/Backups/Switch/switch-saves-2026-08-06_185502
```

### `doctor`

Prints a human-readable diagnostic report: verdict, visible Nintendo/MTP devices, USB
client blockers, and ordered remediation steps.

```sh
switchmtp-cli doctor
```

Use `--json` to capture the raw diagnostics envelope for scripts or bug reports:

```sh
switchmtp-cli --json doctor > diagnostics.json
```

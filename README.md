# Blockforge

Installer and updater for Minecraft servers.

Blockforge has two modes:

- manifest mode: installs/updates modded servers from a Blockforge manifest
- vanilla mode: installs/updates Mojang's latest recommended vanilla Java server

Manifest contract comes from:

```text
https://rannday.github.io/blockforge-manifest/manifest.schema.json
```

## Requirements

- Java 21+
- Network access
- Linux, macOS, or Windows binary for target platform
- GitHub CLI (`gh`) for release uploads

## Install

```bash
tar -xzf blockforge-0.1.4-linux-amd64.tar.gz
sudo install -m 0755 blockforge /usr/local/bin/blockforge
```

Release archives contain only the platform binary.

## Usage

Manifest mode first install requires a manifest source. `--manifest` accepts an HTTP URL, HTTPS URL, `file://` URL, or local path:

```bash
blockforge --dir /opt/minecraft/my-pack --manifest https://example.com/manifest.json
```

Local test:

```powershell
go run .\cmd\blockforge --dir .\tmp --manifest "C:\Users\rannd\Downloads\varda-test-ars-manifest.json"
```

Later updates reuse the saved source from `.blockforge/manifest-url`:

```bash
blockforge --dir /opt/minecraft/my-pack
```

Passing `--manifest` again uses that source and saves its normalized form for later runs. Blockforge always reads the manifest source during install/update; it does not persist the full manifest as desired state.

Vanilla mode uses Mojang/Piston metadata and always targets `latest.release`. There is no user-selectable Minecraft version flag and snapshots are not supported:

```bash
blockforge --vanilla --dir /opt/minecraft/vanilla
blockforge --vanilla -d /opt/minecraft/vanilla
blockforge --vanilla --dir /opt/minecraft/vanilla --dry-run
blockforge --vanilla --dir /opt/minecraft/vanilla --force
```

`--vanilla` cannot be combined with `-m`, `--manifest`, `--manifest-url`, `-c`, or `--check-manifest`. It writes vanilla state under `.blockforge` and does not write `.blockforge/manifest-url`.

Flags:

- `-m`, `--manifest`
- `-d`, `--dir`
- `-c`, `--check-manifest`
- `--vanilla`
- `--dry-run`
- `-f`, `--force`
- `-w`, `--workers`
- `-v`, `--version`

`--check-manifest` validates the resolved manifest and prints a summary without changing files. It can use either `--manifest` or the saved local manifest source.

Dry run validates the resolved manifest and prints planned changes without modifying files:

```bash
blockforge --dir /opt/minecraft/my-pack --dry-run
```

Unmanaged files and directories in `mods/` are listed as planned removals but are not removed during dry run.

## Managed Files

Manifest schema is only for modded modpacks. It does not support vanilla. Vanilla desired state comes from Mojang/Piston `latest.release` metadata instead.

Blockforge treats `mods/` as manifest-managed. Mods not listed in the manifest are removed during reconciliation. If you add local extra mods, they must be re-added after each update or included in the manifest.

`server_config` is reapplied when manifest root `version` changes. `--force` reapplies `server_config` even when the version is unchanged.

Current loader install support is NeoForge. Forge, Fabric, and Quilt are recognized by the manifest schema, but this Blockforge version returns a clear not-implemented error for them.

Vanilla mode manages:

- `server.jar`
- `run.sh`
- `run.bat`
- `user_jvm_args.txt`
- `.blockforge/install-type`
- `.blockforge/minecraft-version`
- `.blockforge/server-jar-sha1`
- `.blockforge/installer-version.txt`

Vanilla mode rejects directories with `.blockforge/manifest-url` so modded and vanilla installs are not mixed.

## OpenRC Example

```sh
#!/sbin/openrc-run

name="Minecraft Server"
description="Modded Minecraft server"

command="/opt/minecraft/my-pack/run.sh"
command_user="minecraft:minecraft"
directory="/opt/minecraft/my-pack"
pidfile="/run/my-pack-minecraft.pid"
command_background="yes"

depend() {
  need net
  after firewall
}

start_pre() {
  ebegin "Updating Minecraft server"
  /usr/local/bin/blockforge --dir /opt/minecraft/my-pack
  eend $?
}
```

## Release Flow

```bash
go generate ./...
go test ./...
go tool go-build-bin -v 0.1.4 --name blockforge --main ./cmd/blockforge --version-var github.com/rannday/blockforge/internal/serverinstaller.Version --clean
gh release create v0.1.4 tmp/release/0.1.4/* --repo rannday/blockforge --title "v0.1.4" --notes "Blockforge 0.1.4"
```

After `go generate ./...`, use:

```bash
git diff --exit-code
```

Schema updates from `blockforge-manifest` flow into this repo via `go generate ./...`.

Integration test:

```bash
go test -tags integration ./internal/tests/blockforge -v
```

Requires Java 21+, network access, and enough time to download NeoForge plus the test mod.

If release already exists, upload/replace assets with:

```bash
gh release upload v0.1.4 tmp/release/0.1.4/* --repo rannday/blockforge --clobber
```

Build tool:

- comes from `github.com/rannday/go-build-bin`,
- builds platform binaries with size-reducing Go flags,
- packages release archives under `tmp/release/<version>`,
- writes `checksums.txt`.

Uploader:

- is GitHub CLI (`gh`),
- uploads release archives and `checksums.txt`.

Final artifact names:

```text
blockforge-0.1.4-windows-amd64.zip
blockforge-0.1.4-linux-amd64.tar.gz
blockforge-0.1.4-linux-arm64.tar.gz
blockforge-0.1.4-darwin-amd64.tar.gz
blockforge-0.1.4-darwin-arm64.tar.gz
checksums.txt
```

## blockforge-manifest

`blockforge-manifest` owns the manifest schema and published example manifests. This repo only publishes installer archives and `checksums.txt`.

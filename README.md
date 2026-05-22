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

- Java version required by the target server metadata. Vanilla reads Mojang/Piston `javaVersion.majorVersion`; current NeoForge manifest installs still require Java 21+.
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
blockforge --vanilla --dir /opt/minecraft/vanilla --java /path/to/java
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
- `-j`, `--java` (vanilla only for now)
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
- `.blockforge/java-major-version`
- `.blockforge/installer-version.txt`

Vanilla mode rejects directories with `.blockforge/manifest-url` so modded and vanilla installs are not mixed.

## blockforge-manifest

`blockforge-manifest` owns the manifest schema and published example manifests. This repo only publishes installer archives and `checksums.txt`.

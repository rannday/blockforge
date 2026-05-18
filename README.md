# Blockforge

Standalone installer and updater for the Varda modded Minecraft server.

It fetches desired server state from:

```text
https://rannday.github.io/blockforge-manifest/manifest.json
```

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

```bash
blockforge --dir /opt/minecraft/varda
```

Flags:

- `--manifest-url`
- `--check-manifest`
- `--force`
- `--download-workers`
- `--version`

Installer fetches a remote manifest.

## OpenRC

```sh
#!/sbin/openrc-run

name="Varda Minecraft Server"
description="Varda modded Minecraft server"

command="/opt/minecraft/varda/run.sh"
command_user="minecraft:minecraft"
directory="/opt/minecraft/varda"
pidfile="/run/varda-minecraft.pid"
command_background="yes"

depend() {
  need net
  after firewall
}

start_pre() {
  ebegin "Updating Varda Minecraft server"
  /usr/local/bin/blockforge --dir /opt/minecraft/varda
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

If release already exists, upload/replace assets with:

```bash
gh release upload v0.1.4 tmp/release/0.1.4/* --repo rannday/blockforge --clobber
```

Build tool:

- comes from `github.com/rannday/go-build-bin/cmd/go-build-bin`,
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

`blockforge-manifest` owns the manifest schema and desired server manifest. This repo only publishes installer archives and `checksums.txt`.

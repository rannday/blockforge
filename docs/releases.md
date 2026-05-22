# Releases

```bash
go generate ./...
go vet ./...
go test ./...
go test -tags integration ./internal/tests/blockforge -v
go tool go-build-bin -v 0.1.4 --name blockforge --main ./cmd/blockforge --version-var github.com/rannday/blockforge/internal/serverinstaller.Version --clean
gh release create v0.1.4 tmp/release/0.1.4/* --repo rannday/blockforge --title "v0.1.4" --notes "Blockforge 0.1.4"
```

After `go generate ./...`, use:

```bash
git diff --exit-code
```

Schema updates from `blockforge-manifest` flow into this repo via `go generate ./...`.

Requires suitable Java runtimes for the integration fixtures, network access, and enough time to download NeoForge plus the test mod.

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

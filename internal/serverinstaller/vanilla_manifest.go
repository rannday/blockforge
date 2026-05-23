package serverinstaller

import (
	"context"
	"encoding/json"
	"fmt"
)

const vanillaVersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type VanillaServer struct {
	MinecraftVersion string
	JavaMajorVersion int
	ServerURL        string
	ServerSHA1       string
	ServerSize       int64
}

type mojangVersionManifest struct {
	Latest struct {
		Release string `json:"release"`
	} `json:"latest"`
	Versions []struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	} `json:"versions"`
}

type mojangVersionJSON struct {
	JavaVersion struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads map[string]struct {
		URL  string `json:"url"`
		SHA1 string `json:"sha1"`
		Size int64  `json:"size"`
	} `json:"downloads"`
}

func FetchLatestVanillaServer() (VanillaServer, error) {
	return fetchLatestVanillaServer(context.Background(), defaultDeps)
}

func fetchLatestVanillaServer(ctx context.Context, deps runtimeDeps) (VanillaServer, error) {
	deps = deps.withDefaults()
	raw, err := downloadURLToBytesCtx(ctx, deps, vanillaVersionManifestURL)
	if err != nil {
		return VanillaServer{}, fmt.Errorf("fetch vanilla version manifest: %w", err)
	}

	var manifest mojangVersionManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return VanillaServer{}, fmt.Errorf("parse vanilla version manifest: %w", err)
	}
	if manifest.Latest.Release == "" {
		return VanillaServer{}, fmt.Errorf("vanilla version manifest missing latest.release")
	}

	var versionURL string
	foundVersion := false
	for _, version := range manifest.Versions {
		if version.ID == manifest.Latest.Release {
			foundVersion = true
			versionURL = version.URL
			break
		}
	}
	if !foundVersion {
		return VanillaServer{}, fmt.Errorf("latest release %q not found in vanilla version manifest", manifest.Latest.Release)
	}
	if versionURL == "" {
		return VanillaServer{}, fmt.Errorf("latest release %q missing version URL", manifest.Latest.Release)
	}

	raw, err = downloadURLToBytesCtx(ctx, deps, versionURL)
	if err != nil {
		return VanillaServer{}, fmt.Errorf("fetch vanilla version %s: %w", manifest.Latest.Release, err)
	}

	var version mojangVersionJSON
	if err := json.Unmarshal(raw, &version); err != nil {
		return VanillaServer{}, fmt.Errorf("parse vanilla version %s: %w", manifest.Latest.Release, err)
	}
	if version.JavaVersion.MajorVersion <= 0 {
		return VanillaServer{}, fmt.Errorf("vanilla version %s missing javaVersion.majorVersion", manifest.Latest.Release)
	}
	server, ok := version.Downloads["server"]
	if !ok {
		return VanillaServer{}, fmt.Errorf("vanilla version %s missing downloads.server", manifest.Latest.Release)
	}
	if server.URL == "" {
		return VanillaServer{}, fmt.Errorf("vanilla version %s missing downloads.server.url", manifest.Latest.Release)
	}
	if server.SHA1 == "" {
		return VanillaServer{}, fmt.Errorf("vanilla version %s missing downloads.server.sha1", manifest.Latest.Release)
	}
	if err := validateSHA1Hex(server.SHA1, "downloads.server.sha1"); err != nil {
		return VanillaServer{}, err
	}
	if server.Size <= 0 {
		return VanillaServer{}, fmt.Errorf("vanilla version %s missing downloads.server.size", manifest.Latest.Release)
	}

	return VanillaServer{
		MinecraftVersion: manifest.Latest.Release,
		JavaMajorVersion: version.JavaVersion.MajorVersion,
		ServerURL:        server.URL,
		ServerSHA1:       server.SHA1,
		ServerSize:       server.Size,
	}, nil
}

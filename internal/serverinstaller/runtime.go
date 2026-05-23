package serverinstaller

import (
	"crypto/tls"
	"net/http"
	"os"
	"os/exec"
	"time"
)

type runtimeDeps struct {
	httpClient *http.Client
	stat       func(string) (os.FileInfo, error)
	rename     func(string, string) error
	remove     func(string) error
	command    func(string, ...string) *exec.Cmd
	lookPath   func(string) (string, error)
}

var defaultDeps = runtimeDeps{
	httpClient: defaultHTTPClient(),
	stat:       os.Stat,
	rename:     os.Rename,
	remove:     os.Remove,
	command:    exec.Command,
	lookPath:   exec.LookPath,
}

func (deps runtimeDeps) withDefaults() runtimeDeps {
	if deps.httpClient == nil {
		deps.httpClient = defaultDeps.httpClient
	}
	if deps.stat == nil {
		deps.stat = defaultDeps.stat
	}
	if deps.rename == nil {
		deps.rename = defaultDeps.rename
	}
	if deps.remove == nil {
		deps.remove = defaultDeps.remove
	}
	if deps.command == nil {
		deps.command = defaultDeps.command
	}
	if deps.lookPath == nil {
		deps.lookPath = defaultDeps.lookPath
	}
	return deps
}

func defaultHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
}

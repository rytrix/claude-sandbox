// claude-sandbox runs Claude Code in a rootless Podman sandbox.
//
//   - Only the current directory is mounted (at the same path).
//   - The container gets its own persistent home; your real home is never mounted.
//   - The container has no direct network access and no DNS. All traffic goes
//     through a small proxy container that only allows the domains in the allowlist.
//
// Usage:
//
//	claude-sandbox [claude args...]   start Claude Code in $PWD (args pass through)
//	claude-sandbox shell [args...]    open a bash shell in the same sandbox
//	claude-sandbox build              rebuild both images, e.g. to update Claude Code
//	claude-sandbox logs               follow the proxy log (shows refused domains)
//	claude-sandbox test               check that the network isolation works
//
// Config:  config/ in this repository (Containerfiles, allowlist, tinyproxy.conf),
//
//	embedded at build time. After editing it, rebuild claude-sandbox; the
//	images are rebuilt and the proxy restarted on the next run.
//
// Home:    ~/.local/share/claude-sandbox/home
//
// Install: go build -o ~/.local/bin/claude-sandbox .

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	proxyName = "claude-sandbox-proxy"
	netName   = "claude-sandbox-net"
	subnet    = "10.89.77.0/24"
	proxyIP   = "10.89.77.2" // fixed address of the proxy on the sandbox network
	proxyURL  = "http://" + proxyIP + ":8888"
)

type config struct {
	Home    string // your real home directory (never mounted)
	BoxHome string // the sandbox's persistent home, mounted at Home
	Workdir string // the current directory, mounted at the same path

	Files map[string][]byte // the embedded config files, rendered
}

func loadConfig() (config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return config{}, err
	}
	workdir, err := os.Getwd()
	if err != nil {
		return config{}, err
	}
	dataHome := envOr("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))

	c := config{
		Home:    home,
		BoxHome: filepath.Join(dataHome, "claude-sandbox", "home"),
		Workdir: workdir,
	}
	c.Files, err = loadFiles()
	return c, err
}

// envOr returns the environment variable key, or def if it is unset or empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "claude-sandbox:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if _, err := exec.LookPath("podman"); err != nil {
		return errors.New("podman is not installed")
	}
	c, err := loadConfig()
	if err != nil {
		return err
	}

	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "build":
		for _, img := range []image{proxyImage, boxImage} {
			if err := buildImage(c, img); err != nil {
				return err
			}
		}
		stopProxy()
		fmt.Println("claude-sandbox: images built")
		return nil
	case "logs":
		return execPodman("logs", "-f", proxyName)
	}

	// The other commands all run a sandbox container in the current directory.
	if err := checkWorkdir(c); err != nil {
		return err
	}
	if err := prepare(c); err != nil {
		return err
	}
	switch cmd {
	case "test":
		return selfTest(c)
	case "shell":
		return execBox(c, append([]string{"bash"}, args[1:]...))
	default:
		return execBox(c, append([]string{"claude"}, args...))
	}
}

// prepare makes sure the box home, the images, the network and the proxy
// exist and are up to date.
func prepare(c config) error {
	if err := os.MkdirAll(c.BoxHome, 0o755); err != nil {
		return err
	}
	for _, img := range []image{proxyImage, boxImage} {
		if err := ensureImage(c, img); err != nil {
			return err
		}
	}
	if err := ensureNetwork(); err != nil {
		return err
	}
	return ensureProxy(c)
}

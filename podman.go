package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// podman runs a podman command with its output going to the terminal.
func podman(args ...string) error {
	cmd := exec.Command("podman", args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// podmanOutput runs a podman command and returns its stdout. On failure, the
// error includes what podman printed to stderr.
func podmanOutput(args ...string) ([]byte, error) {
	out, err := exec.Command("podman", args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, fmt.Errorf("podman %s: %s", strings.Join(args, " "), bytes.TrimSpace(exitErr.Stderr))
	}
	return out, err
}

// podmanSucceeds runs a podman command that answers yes or no through its exit
// status, such as `podman network exists`.
func podmanSucceeds(args ...string) bool {
	return exec.Command("podman", args...).Run() == nil
}

// execPodman replaces this process with podman, so it gets the terminal and
// signals directly and its exit status becomes ours.
func execPodman(args ...string) error {
	path, err := exec.LookPath("podman")
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{"podman"}, args...), os.Environ())
}

// ---------------------------------------------------------------------------
// Images, network, proxy
// ---------------------------------------------------------------------------

// hashLabel is the image and container label holding the hash of the embedded
// files they were built from.
const hashLabel = "claude-sandbox.files"

type image struct {
	Name  string
	Files []string // the embedded files it is built from; the first is its Containerfile
}

var (
	proxyImage = image{"localhost/claude-sandbox-proxy", []string{"proxy.Containerfile", "tinyproxy.conf", "allowlist"}}
	boxImage   = image{"localhost/claude-sandbox", []string{"Containerfile"}}
)

// buildImage builds img from the embedded files, labelled with their hash.
func buildImage(c config, img image) error {
	dir, err := os.MkdirTemp("", "claude-sandbox-build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	for _, name := range img.Files {
		if err := os.WriteFile(filepath.Join(dir, name), c.Files[name], 0o644); err != nil {
			return err
		}
	}
	return podman("build", "--pull=newer",
		"--label", hashLabel+"="+filesHash(c.Files, img.Files),
		"-t", img.Name, "-f", filepath.Join(dir, img.Files[0]), dir)
}

// ensureImage builds img if it doesn't exist or was built from other files.
func ensureImage(c config, img image) error {
	out, err := podmanOutput("image", "inspect", "-f", `{{index .Labels "`+hashLabel+`"}}`, img.Name)
	if err == nil && strings.TrimSpace(string(out)) == filesHash(c.Files, img.Files) {
		return nil
	}
	fmt.Fprintf(os.Stderr, "claude-sandbox: building %s...\n", img.Name)
	return buildImage(c, img)
}

// ensureNetwork creates the sandbox network. It is --internal, so containers on
// it have no route to the outside world, and --disable-dns, so name lookups
// can't be used to smuggle data out either; the proxy resolves names itself.
// An existing network missing either setting is refused, not used.
func ensureNetwork() error {
	if !podmanSucceeds("network", "exists", netName) {
		_, err := podmanOutput("network", "create", "--internal", "--disable-dns", "--subnet", subnet, netName)
		return err
	}
	out, err := podmanOutput("network", "inspect", netName)
	if err != nil {
		return err
	}
	var nets []struct {
		Internal   bool `json:"internal"`
		DNSEnabled bool `json:"dns_enabled"`
	}
	if err := json.Unmarshal(out, &nets); err != nil {
		return fmt.Errorf("reading podman network inspect %s: %w", netName, err)
	}
	if len(nets) != 1 || !nets[0].Internal || nets[0].DNSEnabled {
		return fmt.Errorf("network %s is not internal and without DNS; remove it (podman rm -f %s && podman network rm %s) and try again", netName, proxyName, netName)
	}
	return nil
}

func stopProxy() {
	podmanOutput("rm", "-f", proxyName)
}

// ensureProxy starts the proxy, or restarts it if it runs with an outdated
// config (e.g. an allowlist from before claude-sandbox was rebuilt).
func ensureProxy(c config) error {
	hash := filesHash(c.Files, proxyImage.Files)
	out, err := podmanOutput("inspect", "-f", `{{.State.Running}} {{index .Config.Labels "`+hashLabel+`"}}`, proxyName)
	if err == nil && strings.TrimSpace(string(out)) == "true "+hash {
		return nil
	}
	stopProxy()
	// The proxy is the only container on both networks: the default one
	// (internet) and the internal sandbox network, where it has a fixed address.
	_, err = podmanOutput("run", "-d", "--name", proxyName,
		"--label", hashLabel+"="+hash,
		"--network", "podman",
		"--network", netName+":ip="+proxyIP,
		"--read-only",
		"--security-opt", "label=disable",
		proxyImage.Name)
	if err != nil {
		return err
	}
	time.Sleep(time.Second) // give tinyproxy a moment to start listening
	return nil
}

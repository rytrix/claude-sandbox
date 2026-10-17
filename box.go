package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// checkWorkdir refuses to mount your home directory or anything that contains
// it (incl. /). Symlinks are resolved first, so a path like /var/home is
// refused too on systems where /home links to it.
func checkWorkdir(c config) error {
	dir, home := resolvePath(c.Workdir), resolvePath(c.Home)
	if strings.HasPrefix(home+"/", strings.TrimSuffix(dir, "/")+"/") {
		return fmt.Errorf("refusing to mount %s (it contains your home directory)", c.Workdir)
	}
	return nil
}

// resolvePath returns p with all symlinks resolved, or just cleaned if it
// can't be resolved (e.g. because it doesn't exist).
func resolvePath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// boxArgs returns the `podman run` options shared by every sandbox container.
func boxArgs(c config) []string {
	return []string{
		"--hostname", "claude-box",
		"--network", netName,
		"--userns=keep-id",
		"--cap-drop=all",
		"--security-opt", "no-new-privileges",
		"--security-opt", "label=disable",
		"--init",
		"--tz=local",
		"-e", "HOME=" + c.Home,
		"-e", "TERM=" + envOr("TERM", "xterm-256color"),
		"-e", "COLORTERM=" + os.Getenv("COLORTERM"),
		"-e", "HTTPS_PROXY=" + proxyURL, "-e", "https_proxy=" + proxyURL,
		"-e", "HTTP_PROXY=" + proxyURL, "-e", "http_proxy=" + proxyURL,
		"-e", "NO_PROXY=localhost,127.0.0.1", "-e", "no_proxy=localhost,127.0.0.1",
		"-e", "DISABLE_AUTOUPDATER=1",
		// Not CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC or DISABLE_TELEMETRY:
		// they also turn off feature-flag fetching, which hides commands
		// such as /usage.
		"-e", "DISABLE_ERROR_REPORTING=1",
		"-v", c.BoxHome + ":" + c.Home,
		"-v", c.Workdir + ":" + c.Workdir,
		"-w", c.Workdir,
	}
}

// execBox replaces this process with a sandbox container running command.
func execBox(c config, command []string) error {
	args := []string{"run", "--rm", "-i"}
	if isTerminal(os.Stdin) && isTerminal(os.Stdout) {
		args = append(args, "-t")
	}
	args = append(args, boxArgs(c)...)
	args = append(args, boxImage.Name)
	return execPodman(append(args, command...)...)
}

// isTerminal reports whether f is a terminal (strictly, a character device).
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ---------------------------------------------------------------------------
// claude-box test
// ---------------------------------------------------------------------------

// selfTest starts a sandbox container and checks its network isolation from
// inside it.
func selfTest(c config) error {
	// The container sleeps for a limited time, so --rm cleans it up even if
	// claude-box is interrupted before its own cleanup runs.
	args := append([]string{"run", "-d", "--rm"}, boxArgs(c)...)
	out, err := podmanOutput(append(args, boxImage.Name, "sleep", "120")...)
	if err != nil {
		return err
	}
	id := strings.TrimSpace(string(out))
	defer podmanOutput("rm", "-f", "-t", "0", id)

	curl := func(args ...string) []string {
		return append([]string{"curl", "-sS", "--max-time", "10", "-o", "/dev/null"}, args...)
	}
	checks := []struct {
		wantReachable bool
		desc          string
		cmd           []string
	}{
		{true, "api.anthropic.com through the proxy", curl("https://api.anthropic.com")},
		{false, "example.com through the proxy", curl("https://example.com")},
		{false, "example.com directly, bypassing the proxy", curl("--noproxy", "*", "https://example.com")},
		// --fail, or curl counts the proxy's own 403 "Filtered" page as a success.
		{false, "plain HTTP through the proxy", curl("--fail", "http://api.anthropic.com")},
		{false, "DNS lookup of example.com", []string{"timeout", "10", "getent", "hosts", "example.com"}},
	}

	failed := 0
	for _, check := range checks {
		reachable := podmanSucceeds(append([]string{"exec", id}, check.cmd...)...)
		result, got := "PASS", "blocked"
		if reachable {
			got = "reachable"
		}
		if reachable != check.wantReachable {
			result = "FAIL"
			failed++
		}
		fmt.Printf("%s  %-9s  %s\n", result, got, check.desc)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d checks failed", failed, len(checks))
	}
	return nil
}

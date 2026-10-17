# claude-sandbox

`claude-sandbox` runs [Claude Code](https://claude.com/claude-code) inside a rootless
Podman sandbox, so it can work on a project without access to the rest of your
machine or the open internet.

- **Only the current directory is mounted**, at the same path as on the host.
  Running it in your home directory, or any directory that contains it
  (including `/`), is refused.
- **Your real home is never mounted.** The sandbox gets its own persistent home
  at `~/.local/share/claude-sandbox/home` (or `$XDG_DATA_HOME/claude-sandbox/home`), so
  Claude Code's login and settings survive between runs.
- **No direct network access and no DNS.** The sandbox container sits on an
  internal Podman network. Its only way out is a small
  [tinyproxy](https://tinyproxy.github.io/) container that allows HTTPS to the
  domains in an allowlist and refuses everything else, including all plain HTTP.
- **Reduced privileges.** The container drops all capabilities, runs with
  `no-new-privileges`, and maps your user ID into the container
  (`--userns=keep-id`), so files it creates belong to you.

## How it works

```
┌─────────────── claude-sandbox-net (internal, no DNS) ───────────────┐
│                                                                     │
│  claude-sandbox container ──HTTPS_PROXY──▶ claude-sandbox-proxy ────┼──▶ internet
│  (Claude Code, $PWD mounted)               10.89.77.2:8888          │    (allowlisted
│                                            (tinyproxy)              │     hosts only)
└─────────────────────────────────────────────────────────────────────┘
```

The proxy is the only container on both the internal sandbox network and
Podman's default network. It resolves host names itself and only opens
`CONNECT` tunnels to port 443 on hosts that match the allowlist.

Both images are built from the files in [`config/`](config/), which are
embedded into the `claude-sandbox` binary at build time. Each image and the proxy
container are labelled with a hash of the files they were built from; when you
rebuild `claude-sandbox` with changed files, the affected image is rebuilt and the
proxy restarted on the next run.

## Requirements

- Linux with [Podman](https://podman.io/) (rootless)
- Go 1.22 or later to build

## Install

```sh
go build -o ~/.local/bin/claude-sandbox .
```

Make sure `~/.local/bin` is on your `PATH`. The images are built automatically
the first time you run `claude-sandbox`.

## Usage

Run it from the project directory you want Claude Code to work on:

```sh
cd ~/src/my-project
claude-sandbox                  # start Claude Code; any arguments pass through
claude-sandbox --resume         # e.g. arguments for claude
```

| Command                    | Description                                              |
| -------------------------- | -------------------------------------------------------- |
| `claude-sandbox [args...]`     | Start Claude Code in the current directory               |
| `claude-sandbox shell [args]`  | Open a bash shell in the same sandbox                    |
| `claude-sandbox build`         | Rebuild both images, e.g. to update Claude Code          |
| `claude-sandbox logs`          | Follow the proxy log (shows refused domains)             |
| `claude-sandbox test`          | Check that the network isolation works                   |

Claude Code's auto-updater is disabled inside the sandbox; run
`claude-sandbox build` to pull the latest version.

### Checking the isolation

`claude-sandbox test` starts a sandbox container and verifies from inside it that:

| Check                                       | Expected  |
| ------------------------------------------- | --------- |
| `api.anthropic.com` through the proxy       | reachable |
| `example.com` through the proxy             | blocked   |
| `example.com` directly, bypassing the proxy | blocked   |
| Plain HTTP through the proxy                | blocked   |
| DNS lookup of `example.com`                 | blocked   |

## Configuration

All configuration lives in [`config/`](config/). After editing any of it, run
`go build` again; the changes take effect on the next run.

| File                                                  | Purpose                                         |
| ----------------------------------------------------- | ----------------------------------------------- |
| [`config/allowlist`](config/allowlist)                | Domains the sandbox may reach over HTTPS        |
| [`config/Containerfile`](config/Containerfile)        | The sandbox image (Arch Linux + Claude Code)    |
| [`config/proxy.Containerfile`](config/proxy.Containerfile) | The proxy image (Alpine + tinyproxy)       |
| [`config/tinyproxy.conf.tmpl`](config/tinyproxy.conf.tmpl) | tinyproxy configuration (Go template)      |

### Allowing more domains

The allowlist holds one extended regular expression per line, matched against
the `host:port` of each HTTPS connection. Always anchor patterns so that, for
example, `api.anthropic.com.evil.com` can't slip through:

```
^registry\.npmjs\.org:443$
```

The allowlist ships with the hosts Claude Code needs, some optional Anthropic
hosts, and commented-out entries for common package registries (npm, PyPI).
`crates.io` is enabled. Use `claude-sandbox logs` to see which domains are being
refused.

### Adding tools to the sandbox

The sandbox image is based on Arch Linux with git, ripgrep, neovim, Python and
`base-devel`. Add the toolchains your projects need to
[`config/Containerfile`](config/Containerfile), for example:

```dockerfile
RUN pacman -S --noconfirm --needed go rust nodejs npm && pacman -Scc --noconfirm
```

## Development

```sh
go test ./...
```

## Resetting

To remove everything `claude-sandbox` created:

```sh
podman rm -f claude-sandbox-proxy
podman network rm claude-sandbox-net
podman rmi localhost/claude-sandbox localhost/claude-sandbox-proxy
rm -rf ~/.local/share/claude-sandbox   # the sandbox home, incl. Claude Code login
```

## License

[MIT](LICENSE)

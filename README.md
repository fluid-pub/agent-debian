# fluid-pub/agent-debian

Fluid **execution agent** for Debian-like hosts: WebSocket connection to the control plane, apt/dpkg skills, workspace/terraform helpers, and child-agent bootstrap (`debian.fluid_execution_agent.bootstrap`).

## Repository layout

| Path | Role |
|------|------|
| `core/` | Git submodule → [`fluid-pub/agent-core`](https://github.com/fluid-pub/agent-core) |
| `cmd/` | Entrypoint and `cmd/version.go` (semver for releases) |
| `internal/` | Agent runtime, Debian skills, config |
| `config/agent.example.yml` | Configuration template |
| `.github/workflows/` | CI and release via [`fluid-pub/actions`](https://github.com/fluid-pub/actions) |

## Local development

One-time per clone, enable the same **`gofmt`** check as CI:

```bash
./scripts/install-git-hooks.sh
```

```bash
git submodule update --init --recursive
cp config/agent.example.yml config/agent.yml
cp env.secrets.example env.secrets
# Set FLUID_CONTROLPLANE_* and enrollment values in env.secrets (never commit that file).
source env.secrets
go test ./...
go run ./cmd -config config/agent.yml
```

**Monorepo Fluid** (`code/agents/debian/`): use `go mod edit -replace fluid/agents/core=../core` for local core without submodule. Do not commit `../core` on `develop` (CI uses `replace => ./core`).

## Enrollment (first boot)

1. Provision an enrollment token for `{ "principal": "execution_agent", "agent_type": "debian" }`.
2. Set `FLUID_ENROLLMENT_TOKEN`, `FLUID_CONTROLPLANE_HTTP_BASE`, and `FLUID_CONTROLPLANE_WEBSOCKET_URL` (e.g. `/etc/fluid/debian/enrollment.env`).
3. Run `fluid-agent-debian` with `-credentials /etc/fluid/debian/credentials.yaml` (default). See `env.secrets.example` and control plane doc `agent_enrollment_bootstrap.md`.

## Release

Semver tags (`0.y.z`, no `v` prefix) publish `ghcr.io/fluid-pub/agent-debian:<tag>` and GitHub Release assets (`fluid-agent-debian-linux-amd64`).

Report security issues via [`SECURITY.md`](SECURITY.md).

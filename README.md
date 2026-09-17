<h1 align="center">osg-gateway</h1>

<p align="center">
  <strong>Control-plane registry & relay</strong><br>
  HTTP registry for sandboxes, providers, policy, proposals, and relayed exec.
</p>
<p align="center">
  <a href="https://github.com/zorneth/osg-gateway/actions/workflows/ci.yml"><img src="https://github.com/zorneth/osg-gateway/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://pkg.go.dev/github.com/zorneth/osg-gateway"><img src="https://pkg.go.dev/badge/github.com/zorneth/osg-gateway.svg" alt="Go Reference"></a>
  <a href="https://opensource.org/licenses/MIT"><img src="https://img.shields.io/badge/License-MIT-yellow.svg" alt="License"></a>
  <a href="https://github.com/zorneth/osg-gateway"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?logo=go" alt="Go Version"></a>

  <a href="https://github.com/zorneth/osg-gateway/actions/workflows/images-gateway.yml"><img src="https://github.com/zorneth/osg-gateway/actions/workflows/images-gateway.yml/badge.svg" alt="images-gateway"></a>
</p>
<p align="center">
  <sub>Part of the <a href="https://github.com/zorneth">zorneth / osg</a> ecosystem</sub>
</p>

---

## Overview

**osg-gateway** is the optional control-plane daemon. Sandboxes register here; the CLI and SDKs talk HTTP for inventory, effective policy, provider attach, policy proposals, logs, and relayed exec.

### Key Features

| Category | Capabilities |
|----------|--------------|
| **Registry** | Sandbox upsert / list / delete |
| **Policy** | Base + effective policy; provider attach |
| **Proposals** | Store / approve / reject (`policy.local` sync) |
| **Relay** | Long-poll exec for `osg-agent` guests |
| **Image** | `ghcr.io/zorneth/osg/gateway` |

---

## Installation

```bash
go install github.com/zorneth/osg-gateway/cmd/osg-gateway@latest
# or:
go build -o osg-gateway ./cmd/osg-gateway
./osg-gateway --listen 127.0.0.1:7443
```

**Requirements:** Go 1.27+

**Container:** `ghcr.io/zorneth/osg/gateway:latest`

---

## Quick Start

```bash
./osg-gateway --listen 127.0.0.1:7443 &
osg gateway add http://127.0.0.1:7443 --local --name local
osg gateway select local
curl -s http://127.0.0.1:7443/healthz
```

### HTTP API (selected)

| Method | Path | Role |
|--------|------|------|
| GET | `/healthz` | liveness |
| GET | `/v1/info` | gateway id + sandbox count |
| GET/PUT/DELETE | `/v1/sandboxes/{name}` | registry |
| GET/PUT | `/v1/sandboxes/{name}/policy` | base / effective policy |
| GET/POST | `/v1/sandboxes/{name}/proposals` | policy advisor chunks |
| GET | `/v1/profiles` | provider profiles |

Durable state: `$XDG_STATE_HOME/osg/gateway/state.json`.

---

## Package Structure

| Path | Purpose |
|------|---------|
| `cmd/osg-gateway` | Daemon entrypoint |
| `internal/` | HTTP handlers, state, relay |


---

## Related

| Resource | Link |
|----------|------|
| Organization | [https://github.com/zorneth](https://github.com/zorneth) |
| Organization overview | [github.com/zorneth](https://github.com/zorneth) |
| pkg.go.dev | [`github.com/zorneth/osg-gateway`](https://pkg.go.dev/github.com/zorneth/osg-gateway) |

## License

[MIT](./LICENSE) © zorneth

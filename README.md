# osg-gateway

MIT · Control-plane daemon (P8 MVP).

```bash
go build -C osg-gateway -o osg-gateway ./cmd/osg-gateway
./osg-gateway --listen 127.0.0.1:7443
# optional TLS: --tls-cert cert.pem --tls-key key.pem
```

## HTTP API

| Method | Path | Role |
|--------|------|------|
| GET | `/healthz` | liveness |
| GET | `/v1/info` | gateway id + sandbox count |
| GET | `/v1/sandboxes` | list registered sandboxes |
| GET/PUT/DELETE | `/v1/sandboxes/{name}` | get / upsert / delete |
| GET | `/v1/sandboxes/{name}/effective-policy` | effective policy YAML (composition) |
| GET | `/v1/sandboxes/{name}/policy?view=base\|full` | base policy or effective policy |
| PUT | `/v1/sandboxes/{name}/policy` | set base; validate effective candidate; return effective |
| PUT/DELETE | `/v1/sandboxes/{name}/providers/{provider}` | attach / detach |
| GET | `/v1/profiles` | list builtin + custom profiles |
| GET/PUT/DELETE | `/v1/profiles/{id}` | get / import / delete custom profile |
| GET | `/v1/providers` | list provider instances |
| GET/PUT/DELETE | `/v1/providers/{name}` | get / create / delete instance (env refs only) |

Durable state: `$XDG_STATE_HOME/osg/gateway/state.json` (or `~/.local/state/osg/gateway`).  
Provider docs: [docs/PROVIDERS.md](../docs/PROVIDERS.md).

## CLI

```bash
osg gateway add local --url http://127.0.0.1:7443
osg gateway select local
osg gateway status
osg sandbox create --name demo --gateway http://127.0.0.1:7443 …
osg gateway list
```

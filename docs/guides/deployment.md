# Deployment

Three deployment shapes, least-to-most exposed:

1. **Loopback-only** (dev / single-user) — the default.
2. **LAN** (team) — bind non-loopback, bearer token, no TLS, restrict by
   firewall.
3. **Public** (shared service) — bind non-loopback, bearer token, TLS at a
   reverse proxy, rate limits, audit log.

## Loopback-only (default)

```bash
sudo ./bin/orchestrator serve
```

No auth, no CORS, listens only on `127.0.0.1:8080`. Use SSH port-forwarding
if you need to hit it from another machine:

```bash
ssh -L 8080:127.0.0.1:8080 jonno@orchestrator-host
```

This is the intended mode for a personal box.

## LAN deployment

```bash
# once:
openssl rand -hex 32 > /etc/orchestrator.token

# /etc/default/orchestrator
ORCHESTRATOR_ADDR=0.0.0.0:8080
ORCHESTRATOR_MCP_ADDR=0.0.0.0:8081
ORCHESTRATOR_AUTH_TOKEN=$(cat /etc/orchestrator.token)
ORCHESTRATOR_CORS_ORIGINS=https://orchestrator.internal
ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl
ORCHESTRATOR_MAX_CONCURRENT_VMS=8
```

Open the ports on UFW:

```bash
sudo ufw allow from 192.168.0.0/16 to any port 8080 comment 'orchestrator api'
sudo ufw allow from 192.168.0.0/16 to any port 8081 comment 'orchestrator mcp'
```

Share the token via your team's secret store (1Password, Vaultwarden, etc.).
The dashboard accepts the token via `#token=…` on the URL, so you can send
a one-shot link that stashes the token into `localStorage` and strips it
from the URL on first load.

## Public deployment (reverse proxy + TLS)

Orchestrator doesn't terminate TLS. Use Caddy, nginx, or Traefik in front.

### Caddy

```caddy
# /etc/caddy/Caddyfile
orchestrator.example.com {
  encode gzip zstd

  # REST + dashboard
  reverse_proxy /api/* 127.0.0.1:8080
  reverse_proxy /*     127.0.0.1:8080

  # MCP on a subpath
  handle_path /mcp/* {
    reverse_proxy 127.0.0.1:8081
  }
}
```

Make sure the orchestrator itself binds **loopback only** when you have a
reverse proxy in front:

```bash
ORCHESTRATOR_ADDR=127.0.0.1:8080
ORCHESTRATOR_MCP_ADDR=127.0.0.1:8081
```

The reverse proxy terminates TLS, orchestrator stays on loopback, and the
bearer token is required because the *effective* bind address for clients is
public. You'll need to set `ORCHESTRATOR_AUTH_TOKEN` even though the
orchestrator thinks it's bound loopback — the proxy forwards the
`Authorization` header untouched.

CORS origin should be the public origin:

```bash
ORCHESTRATOR_CORS_ORIGINS=https://orchestrator.example.com
```

### nginx

```nginx
server {
    listen 443 ssl http2;
    server_name orchestrator.example.com;

    ssl_certificate     /etc/letsencrypt/live/orchestrator.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/orchestrator.example.com/privkey.pem;

    # WebSocket upgrade handling for /api/v1/tasks/*/stream
    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_read_timeout 86400s;  # long-running task streams
    }

    location /mcp/ {
        rewrite ^/mcp(/.*)$ $1 break;
        proxy_pass http://127.0.0.1:8081;
        proxy_set_header Host $host;
    }
}
```

## Hardening checklist

- [ ] Bearer token via `ORCHESTRATOR_AUTH_TOKEN`.
- [ ] TLS terminated by a reverse proxy.
- [ ] CORS origins explicit (`ORCHESTRATOR_CORS_ORIGINS`).
- [ ] UFW / nftables restricting inbound to the proxy.
- [ ] `ORCHESTRATOR_MAX_CONCURRENT_VMS` + `ORCHESTRATOR_TASK_RATE_LIMIT`
      sized to your host.
- [ ] `ORCHESTRATOR_AUDIT_LOG` enabled + shipped to SIEM.
- [ ] `ORCHESTRATOR_WEBHOOK_URL` (optional) to kick off downstream pipelines.
- [ ] `ORCHESTRATOR_EGRESS_ALLOWLIST` limiting guest outbound.
- [ ] Dedicated host (no co-located workloads).
- [ ] Guest credentials via `ANTHROPIC_API_KEY` (not OAuth) in multi-user
      contexts.
- [ ] Rootfs rebuilt from your own cached base image (not live from Debian
      mirrors).

## Multi-host notes

Orchestrator is single-host by design. For multi-host:

- Put a load balancer in front of many orchestrator nodes.
- Use a consistent-hash scheme on task ID → backend so task streams land on
  the same node that started them (the task store is in-memory and not
  shared).
- Or: treat each host as a worker in a higher-level queue (BullMQ, Temporal,
  Pulsar) and don't share dashboards across hosts.

A properly distributed orchestrator is a larger project; happy to accept a
PR if you build one.

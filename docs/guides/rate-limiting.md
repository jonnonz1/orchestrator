# Rate limiting

Two dimensions, both optional.

## Concurrency (semaphore)

```bash
ORCHESTRATOR_MAX_CONCURRENT_VMS=8
```

Caps the number of simultaneously-running VMs. `Acquire()` blocks until a
slot opens or the caller's context expires. Task creation through the REST
API returns 503 with `Retry-After` if the semaphore is full and the client
isn't willing to wait.

**Sizing:** start with `floor(HostRAM / DefaultVMRAM) - 2`. If your default
is 2 GB VMs on a 30 GB host, set this to 12–13. Leave headroom for the
host kernel + orchestrator + Prometheus.

## Rate (token bucket)

```bash
ORCHESTRATOR_TASK_RATE_LIMIT=30
```

The bucket holds up to `N` tokens; each `POST /api/v1/tasks` or
`POST /api/v1/vms` consumes one. The bucket refills at `N/minute`. When the
bucket is empty, the HTTP handler returns `429 Too Many Requests` with
`Retry-After: 60`.

**Sizing:** this is a coarse guard. Set it above your peak legitimate rate
(measure first). Below that, set concurrency instead — rate alone doesn't
bound host RAM.

## Combining them

```bash
ORCHESTRATOR_MAX_CONCURRENT_VMS=8
ORCHESTRATOR_TASK_RATE_LIMIT=30
```

- Concurrency bounds steady-state RAM.
- Rate bounds burst behaviour so a runaway client can't queue 10,000
  requests that each acquire the semaphore one after another.

## HTTP semantics

| Situation | Status | `Retry-After` | Body |
|---|---|---|---|
| Rate bucket empty | 429 | `60` | `{"error":"rate limit exceeded (N tasks/min)"}` |
| Semaphore full, context expired | 503 | `60` | `{"error":"concurrency limit (N) reached: context deadline exceeded"}` |
| Otherwise | — | — | — |

Clients should honour `Retry-After`. The Python and TypeScript SDKs retry
automatically on 429/503.

## What's NOT rate-limited

- `GET` endpoints (cheap reads).
- The WebSocket stream endpoint.
- The embedded frontend asset requests.
- MCP tool calls. An MCP caller that wants back-pressure should honour
  errors returned from individual tools.

## Future: per-caller rate limits

The current limiter is global. A malicious caller with a valid token can
saturate it. If you're running a shared orchestrator, wrap it in an API
gateway (Kong, Tyk, Traefik with a rate-limit middleware) that keys on
token prefix or source IP.

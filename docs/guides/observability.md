# Observability

Four signals: Prometheus metrics, structured logs, audit log, webhooks.

## Prometheus metrics

Scraped from `GET /api/v1/metrics` in Prometheus text format. This endpoint
is **exempt from auth** so Prometheus scrapers don't need the bearer token;
gate it at the network layer if that matters to you.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `orchestrator_vms_running` | gauge | — | Current VMs in `running` state. |
| `orchestrator_tasks_running` | gauge | — | Current tasks in `running` state. |
| `orchestrator_tasks_started_total` | counter | — | Tasks started since process boot. |
| `orchestrator_tasks_completed_total` | counter | `status` (completed/failed) | Tasks finished by outcome. |
| `orchestrator_task_duration_seconds` | histogram | `status` | Time from start to terminal state. |
| `orchestrator_vm_boot_seconds` | histogram | — | Time from `Create()` call to `VMStateRunning`. |
| `orchestrator_stream_bytes_total` | counter | — | Bytes streamed to WebSocket clients. |
| `orchestrator_stream_drops_total` | counter | — | Events dropped due to slow subscribers. |

### Scrape config

```yaml
scrape_configs:
  - job_name: orchestrator
    scrape_interval: 15s
    static_configs:
      - targets: ["orchestrator.internal:8080"]
    metrics_path: /api/v1/metrics
```

### Useful alerts

```yaml
groups:
  - name: orchestrator
    rules:
      - alert: OrchestratorVMsUnbounded
        expr: orchestrator_vms_running > 20
        for: 10m
        annotations:
          summary: "Orchestrator is leaking VMs or under sustained load"

      - alert: OrchestratorTaskFailureRate
        expr: |
          rate(orchestrator_tasks_completed_total{status="failed"}[5m])
          / clamp_min(rate(orchestrator_tasks_completed_total[5m]), 0.001)
          > 0.25
        for: 10m
        annotations:
          summary: "Orchestrator task failure rate >25%"

      - alert: OrchestratorStreamDrops
        expr: rate(orchestrator_stream_drops_total[5m]) > 1
        for: 5m
        annotations:
          summary: "Stream hub dropping events — subscribers too slow"
```

## Structured logs

Set `ORCHESTRATOR_LOG_FORMAT=json` to get one JSON object per log line, ready
for ingestion into Loki, Elasticsearch, etc. Every task-lifecycle log
entry includes `task_id` and `vm` fields so you can group by task.

Example line:

```json
{"time":"2026-04-22T12:34:56Z","level":"INFO","msg":"task completed",
 "id":"a1b2c3d4-e5f6-7890-abcd-1234567890ab","status":"completed",
 "exit_code":0,"duration":"27.3s"}
```

Text format (the default) is easier for a human tailing a file; JSON is
easier for machines.

## Audit log

Set `ORCHESTRATOR_AUDIT_LOG=/var/log/orchestrator.jsonl`. Every task
lifecycle event (`task.started`, `task.completed`, `task.failed`) lands as
a JSON line.

Schema:

```json
{
  "id":"a1b2…",
  "type":"task.completed",
  "timestamp":"2026-04-22T12:35:23.123Z",
  "task_id":"a1b2…",
  "vm_name":"task-a1b2",
  "data": {
    "runtime":"claude",
    "exit_code":0,
    "duration_sec":27.3,
    "cost_usd":0.042,
    "files":["example.png","summary.md"]
  }
}
```

Append-only, 0600 permissions. Rotate with logrotate — see
[Operating the server](operating-server.md).

## Webhooks

Set `ORCHESTRATOR_WEBHOOK_URL=https://hooks.example.com/orchestrator` and
`ORCHESTRATOR_WEBHOOK_SECRET=<hex>`. Every task lifecycle event is POSTed
as JSON with an HMAC-SHA256 signature of the raw body in
`X-Orchestrator-Signature: sha256=<hex>`.

URL scheme is enforced at startup: only `http://` and `https://` are
accepted. Delivery is best-effort (5s timeout, one try, no retries) and
non-blocking — a slow receiver cannot stall the task runner.

### Verifying a webhook in Go

```go
func verify(secret, signatureHeader string, body []byte) bool {
    expected := "sha256=" + hex.EncodeToString(
        hmac.New(sha256.New, []byte(secret)).Sum(body))
    return hmac.Equal([]byte(signatureHeader), []byte(expected))
}
```

### Verifying in Python

```python
import hmac, hashlib
def verify(secret: bytes, sig: str, body: bytes) -> bool:
    mac = hmac.new(secret, body, hashlib.sha256).hexdigest()
    return hmac.compare_digest(sig, "sha256=" + mac)
```

## Correlating the four signals

Every log + audit + webhook event carries the same `task_id`. Build queries
like:

```promql
orchestrator_tasks_running{task_id="a1b2…"}
```

(the metric is aggregate, but the time range lets you locate when the task
was active), then `grep a1b2 /var/log/orchestrator.jsonl` for audit entries,
then look in your webhook receiver for the completion payload. No distributed
tracing, but enough to correlate.

# Data Pipeline (shell runtime — no agent)

**Runtime:** shell · **RAM:** 1024 MB · **Expected duration:** <30 s

Demonstrates: the `shell` runtime for jobs that don't need an LLM — just a sandboxed `bash -c`. Useful for CI-style tasks where you want isolation but don't want to pay the agent cost.

## Prompt

```prompt
set -euo pipefail

# Pretend data source
curl -fsSL https://api.github.com/repos/kubernetes/kubernetes/contributors?per_page=100 -o /tmp/contribs.json

# Transform
python3 - <<'PY'
import json
with open("/tmp/contribs.json") as f:
    d = json.load(f)
top = sorted(d, key=lambda c: -c["contributions"])[:10]
with open("/root/output/top-contributors.md", "w") as f:
    f.write("# Top 10 contributors to kubernetes/kubernetes\n\n")
    for i, c in enumerate(top, 1):
        f.write(f"{i}. **{c['login']}** — {c['contributions']:,} contributions\n")
PY

echo "pipeline done" > /root/output/status.txt
```

Run:

```bash
sudo ./bin/orchestrator task run \
  --prompt "$(sed -n '/^```prompt/,/^```/{//!p;}' examples/data-pipeline.md)" \
  --runtime shell --ram 1024 --vcpus 1 --timeout 60
```

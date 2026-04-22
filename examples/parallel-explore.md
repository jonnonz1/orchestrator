# Parallel Exploration — N Attempts, Pick the Best

**Runtime:** claude · **RAM per task:** 2048 MB · **Concurrency:** 5

Demonstrates: kicking off multiple independent tasks in parallel via the REST API. Each VM is fully isolated; you aggregate results on the host.

## Why

Sometimes you want 5 different attempts at the same problem — different prompts, different temperatures, different strategies — then compare. Orchestrator's per-task isolation makes this safe even when the prompts involve code execution.

## Script

```bash
#!/usr/bin/env bash
# examples/parallel-explore.sh
set -euo pipefail

ORCHESTRATOR_URL="${ORCHESTRATOR_URL:-http://127.0.0.1:8080/api/v1}"
AUTH="Authorization: Bearer ${ORCHESTRATOR_AUTH_TOKEN:-}"

prompts=(
  "Solve FizzBuzz in Python. One-liner if possible."
  "Solve FizzBuzz in Python. Optimise for readability, not brevity."
  "Solve FizzBuzz in Python using match/case."
  "Solve FizzBuzz in Python using a generator."
  "Solve FizzBuzz in Python using functional style (map/filter)."
)

ids=()
for p in "${prompts[@]}"; do
  id=$(curl -sS -H "$AUTH" -H "Content-Type: application/json" \
    -X POST "$ORCHESTRATOR_URL/tasks" \
    -d "{\"prompt\":\"$p\", \"ram_mb\":1024, \"timeout\":60}" \
    | jq -r .id)
  echo "started $id: $p"
  ids+=("$id")
done

echo
echo "waiting for completion..."
for id in "${ids[@]}"; do
  while true; do
    status=$(curl -sS -H "$AUTH" "$ORCHESTRATOR_URL/tasks/$id" | jq -r .status)
    case "$status" in
      completed|failed|cancelled) break ;;
    esac
    sleep 2
  done
  echo "$id → $status"
done
```

## Expected outcome

Five VMs boot in parallel, five sub-Claudes solve FizzBuzz five different ways, five `/root/output/solution.py` files come back to `$ORCHESTRATOR_RESULTS_DIR/<task-id>/`.

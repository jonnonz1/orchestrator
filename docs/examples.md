# Examples

Each file under [`examples/`](https://github.com/jonnonz1/orchestrator/tree/main/examples)
is a ready-to-run task prompt. Copy the prompt into your own task submission
or run it directly via the CLI.

## Catalogue

| Example | Runtime | What it demonstrates |
|---|---|---|
| [screenshot-website](https://github.com/jonnonz1/orchestrator/blob/main/examples/screenshot-website.md) | `claude` | Headless Chromium + Claude tool use |
| [scrape-hacker-news](https://github.com/jonnonz1/orchestrator/blob/main/examples/scrape-hacker-news.md) | `claude` | Python scraping, pagination, JSON output |
| [analyze-github-repo](https://github.com/jonnonz1/orchestrator/blob/main/examples/analyze-github-repo.md) | `claude` | git clone, filesystem analysis, summary report |
| [build-react-app](https://github.com/jonnonz1/orchestrator/blob/main/examples/build-react-app.md) | `claude` | npm init, code generation, live build |
| [run-test-suite](https://github.com/jonnonz1/orchestrator/blob/main/examples/run-test-suite.md) | `claude` | Multi-step build + test reporting |
| [solve-leetcode](https://github.com/jonnonz1/orchestrator/blob/main/examples/solve-leetcode.md) | `claude` | Constrained code generation + verification |
| [refactor-codebase](https://github.com/jonnonz1/orchestrator/blob/main/examples/refactor-codebase.md) | `claude` | Multi-file edits, diff output |
| [pentest-dummy](https://github.com/jonnonz1/orchestrator/blob/main/examples/pentest-dummy.md) | `claude` | Running security tools in an isolated sandbox |
| [data-pipeline](https://github.com/jonnonz1/orchestrator/blob/main/examples/data-pipeline.md) | `shell` | Non-agent batch job via the shell runtime |
| [parallel-explore](https://github.com/jonnonz1/orchestrator/blob/main/examples/parallel-explore.md) | `claude` | Spawning multiple tasks in parallel via the API |

## Running an example from the CLI

Each markdown file has a fenced block tagged `prompt`. Extract and pass:

```bash
sudo ./bin/orchestrator task run \
  --prompt "$(awk '/^```prompt/{flag=1; next} /^```/{flag=0} flag' examples/screenshot-website.md)" \
  --ram 4096
```

## Running via the REST API

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H "Authorization: Bearer $ORCHESTRATOR_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{
        \"prompt\": $(awk '/^```prompt/{flag=1; next} /^```/{flag=0} flag' examples/screenshot-website.md | jq -Rs .),
        \"ram_mb\": 4096
      }"
```

## Contributing a new example

Drop a file into `examples/` with a `prompt`-tagged fenced block and a
short preamble explaining what it demonstrates. Add a row to the table
above. PRs welcome.

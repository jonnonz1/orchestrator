# Orchestrator Examples

Each file here is a ready-to-run task prompt. Use them to kick the tyres, smoke-test a new deployment, or crib as starting points for your own workflows.

Run any example:

```bash
sudo ./bin/orchestrator task run --prompt "$(cat examples/screenshot-website.md | awk '/^```prompt/{flag=1; next} /^```/{flag=0} flag')" --ram 4096
```

Or from the API / MCP:

```bash
curl -X POST http://127.0.0.1:8080/api/v1/tasks \
  -H "Authorization: Bearer $ORCHESTRATOR_AUTH_TOKEN" \
  -H "Content-Type: application/json" \
  -d @examples/screenshot-website.json
```

## Catalogue

| Example                                          | Runtime | What it demonstrates                           |
|--------------------------------------------------|---------|------------------------------------------------|
| [screenshot-website.md](screenshot-website.md)   | claude  | Headless Chromium + Claude tool use            |
| [scrape-hacker-news.md](scrape-hacker-news.md)   | claude  | Python scraping, pagination, JSON output       |
| [analyze-github-repo.md](analyze-github-repo.md) | claude  | git clone, filesystem analysis, summary report |
| [build-react-app.md](build-react-app.md)         | claude  | npm init, code generation, live build          |
| [run-test-suite.md](run-test-suite.md)           | claude  | Multi-step build + test reporting              |
| [solve-leetcode.md](solve-leetcode.md)           | claude  | Constrained code generation + verification     |
| [refactor-codebase.md](refactor-codebase.md)     | claude  | Multi-file edits, diff output                  |
| [pentest-dummy.md](pentest-dummy.md)             | claude  | Running security tools in an isolated sandbox  |
| [data-pipeline.md](data-pipeline.md)             | shell   | Non-agent batch job via the shell runtime      |
| [parallel-explore.md](parallel-explore.md)       | claude  | Spawning multiple tasks in parallel via the API|

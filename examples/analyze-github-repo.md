# Analyze a GitHub Repository

**Runtime:** claude · **RAM:** 4096 MB · **Expected duration:** 60–120 s · **Cost:** ~$0.30–0.60

Demonstrates: `git clone` in the sandbox, filesystem analysis, structured summary reports.

## Prompt

```prompt
Clone https://github.com/sharkdp/bat into /tmp/repo. Then:
1. Count lines of code by language (use cloc if available, otherwise wc)
2. List the 10 most recently modified source files and summarise each in one sentence
3. Identify the main entry point and explain the high-level architecture
4. Write the full report to /root/output/report.md
Do not commit or push anything.
```

## Expected output files

- `report.md` — ~300-line structured analysis

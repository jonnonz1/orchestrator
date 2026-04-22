# Run a Test Suite

**Runtime:** claude · **RAM:** 4096 MB · **Expected duration:** 60–180 s

Demonstrates: cloning, dependency install, running a test runner, parsing + reporting results.

## Prompt

```prompt
Clone https://github.com/expressjs/express into /tmp/repo. Install dev dependencies
with `npm install`. Run `npm test` and parse the output: how many tests passed,
how many failed, which test files had the longest runtimes. Write the report to
/root/output/test-report.md. Also copy the raw npm test output to
/root/output/test-output.txt.
```

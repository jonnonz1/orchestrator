# Refactor a Codebase

**Runtime:** claude · **RAM:** 4096 MB · **Expected duration:** 120–300 s

Demonstrates: multi-file edits, preserving diffs for review, self-limiting scope.

## Prompt

```prompt
Clone https://github.com/tj/commander.js into /tmp/repo and switch to a new branch.
Rename every occurrence of the variable named `_args` in JavaScript source files to
`commandArgs`. Do not touch tests, do not touch files in node_modules, do not add
any new files. When done, run `git diff > /root/output/refactor.diff` and copy it
plus a /root/output/summary.md describing the files touched and any edge cases you hit.
```

# Screenshot a Website

**Runtime:** claude · **RAM:** 4096 MB · **Expected duration:** 30–45 s · **Cost:** ~$0.10–0.20

Demonstrates: headless Chromium inside the VM, Claude choosing the right tool (Playwright / puppeteer / chromium CLI), and result file collection.

## Prompt

```prompt
Take a screenshot of https://example.com at 1920x1080, save it as /root/output/screenshot.png,
and write a short /root/output/description.md describing what you see in the screenshot.
Use the chromium binary at /usr/bin/chromium with --headless and --screenshot.
```

## Expected output files

- `screenshot.png` — full-page PNG of example.com
- `description.md` — 2-3 paragraph description from Claude

## Try it

```bash
sudo ./bin/orchestrator task run \
  --prompt "$(sed -n '/^```prompt/,/^```/{//!p;}' examples/screenshot-website.md)" \
  --ram 4096 --vcpus 2 --timeout 120
```

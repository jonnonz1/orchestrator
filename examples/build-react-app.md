# Build a React App from Scratch

**Runtime:** claude · **RAM:** 4096 MB · **Expected duration:** 90–180 s · **Cost:** ~$0.50–1.00

Demonstrates: npm, code generation across multiple files, `vite build`, HTML/CSS/JS generation.

## Prompt

```prompt
Create a small React + TypeScript app using Vite at /tmp/app. Requirements:
- A login form with email + password, validating via regex
- A "remember me" checkbox whose state persists in localStorage
- TailwindCSS for styling
- No backend — just UI

After building, run `npm run build` and copy the contents of /tmp/app/dist
to /root/output/. Also copy /tmp/app/src/App.tsx and /tmp/app/src/LoginForm.tsx
to /root/output/source/ so a reviewer can read the code.
```

## Expected output files

- `index.html`, `assets/*.js`, `assets/*.css` — the built SPA
- `source/App.tsx`, `source/LoginForm.tsx` — generated source

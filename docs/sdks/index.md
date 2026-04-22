# SDKs

Official clients for the REST API. Use the SDK that matches your language;
fall back to the [OpenAPI spec](https://github.com/jonnonz1/orchestrator/blob/main/docs/openapi.yaml)
to generate a client for anything else.

- **[Python](python.md)** — `pip install orchestrator-sdk`. Stdlib-only
  (no `requests` dependency), synchronous, tiny.
- **[TypeScript](typescript.md)** — `npm install @jonnonz1/orchestrator-sdk`.
  ESM, Node 18+ or modern browsers, uses the platform `fetch` + `WebSocket`.

Both SDKs are thin — they wrap `/api/v1/…` endpoints with ergonomic methods
and handle bearer-token auth, WebSocket streaming, and file retrieval. No
business logic, no retries beyond the basics. Read the source if something
doesn't work; there isn't much of it.

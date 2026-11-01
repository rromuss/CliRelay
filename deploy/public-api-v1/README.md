# External Codex API v1 — deployment gate

This directory is a release candidate, not evidence of deployment. Public access
must stay disabled until private Codex qualification and old-Hermes checks pass.

## Contract

- Public URL: `https://cliprelay.romus.pro:8443/v1`; only GET models,
  POST Responses (HTTP/SSE) and GET `/codex/models` are published.
- The `/external` namespace enforces Bearer authentication, the key-owned
  `external-access` opt-in, explicit model/channel restrictions and positive
  RPM/day/concurrency limits. All existing keys migrate to false.
- Create a separate **Codex — ноутбук** key and profile: the selected Antigravity
  account, `gemini-3.8-flash-tiered`, 10 RPM, 300 started requests/UTC day,
  concurrency 2. Never modify the Hermes key or enable ModelHub fallback.
- Atomic admission is keyed by stable key ID; rotation and restarts do not reset
  the count. Denied requests are not charged against admission; an admitted
  upstream failure/cancellation still counts. SQLite failure is a public 503.
- The external pool has two slots; private Hermes requests do not use it.
  Request bodies are limited to 8 MiB after decompression and deadlines to 120 s.
- External detailed bodies/headers/cookies are not captured. Diagnostic usage
  records use stable identity and a non-credential key label. Existing private
  history and captures are not cleared or reconfigured.
- The bundled catalog is schema-checked with Codex CLI 0.166.0. Other versions
  fail explicitly until their catalog schema and actual tool loop are qualified.
  No promise of live model/tool compatibility is made by catalog presence.

## Required release sequence

1. Record a sanitized baseline of container `hermes`: image, restart count,
   config/model/mode fingerprints, effective private base URL, API-key identity,
   catalog/capability headers. No raw keys, transcripts, OAuth or `.env` copies.
2. Run backend and frontend suites and private Codex qualification: incremental
   SSE, completed terminal event, function/custom/namespace tool loop, patch of
   a disposable file, and a follow-up turn. Do not publish on text-only success.
3. With explicit release/data approval, make a root-only consistent SQLite
   backup and copy only changed configuration; keep the old pinned image.
   Preserve the service UID and close the exact `/opt/CliRelay/{data,logs,auths}`
   directories to other users (0700); SQLite/config/auth files and private backups
   are 0600. Do not chmod shared parents or erase existing history.
   Build from the current SQLite-compatible source, pin the verified panel SHA.
   Do not use upstream `latest`, migrate to PostgreSQL/Redis or start updater.
4. Preserve the exact existing Compose file set and loopback port bindings.
   Create `clirelay-public-ingress`, attach only Caddy and CliRelay, and persist
   that network in each service's Compose definition. No staging network change.
5. During the agreed quiet window drain active requests and replace only
   `cli-proxy-api`. Neither `hermes`, `hermes-staging` nor Xray is restarted.
   First verify the unchanged private Hermes contract with opt-in false.
6. Configure only the requested subdomain's A record to the VPS; no AAAA in v1.
   Append the site block to the existing Caddyfile; validate before graceful
   reload. HTTP-01 uses the existing port 80; do not modify Xray on 443.
   Keep access logging for this host off, or explicitly redact URI query,
   Authorization, cookies and all bodies before enabling it.
7. Enable only the new laptop key. Verify missing/invalid/internal/revoked keys,
   blocked methods/encoded paths, quotas, cancellation and concurrency. Complete
   the actual macOS Codex checks and owner Telegram acceptance before sign-off.

## Old Hermes acceptance

Keep private URL/key/config/models/mode/retry/fallback behavior unchanged. Run
the existing Hermes model-menu/transcription suites and CliRelay Hermes contract
tests. Synthetic fixtures cover tools, image requests and native STT. Live
acceptance before/after release checks text, a tool, voice and an image in the
old bot; the owner performs Telegram interactions. Do not create reminders,
rewrite history or send messages to other people as part of testing.

## Recovery

Close public ingress first, then restore the prior pinned image and Caddy/Compose
configuration. Leave the additive `external_access` column and external usage
tables in place: the prior binary ignores them. Never overwrite the whole DB
with an old backup after new history has been written. Preserve key IDs/counters,
OAuth state, both existing accounts, all Hermes stores and the stopped updater.

Commits, publication, DNS mutation and production release are separate authorized
stages. Keep private backup material and real client configuration out of Git.

## Local qualification evidence (2026-11-01)

- Codex CLI 0.153.0, isolated temporary home/workspace, HTTP/SSE private bridge.
- Five model requests: native local file read, apply_patch (including recovery
  from an invalid patch context), repeated read, final QUALIFIED; exit 0 and the
  disposable file changed exactly as requested.
- Sixth bounded request returned the synthetic marker from replayed history,
  HTTP 200 and response status completed. No service/config changes on the VPS.
- This qualifies the protocol/model on the test machine, not the target laptop
  or the public TLS route.
- The owner updated the target laptop to 0.166.0. That official CLI binary was
  separately qualified with the regenerated catalog: file read, successful
  apply_patch, verification read and a final answer (exit 0). A resumed turn in
  the same conversation recalled the synthetic marker without tools (exit 0).
- Full backend Go suite passed; targeted migration, admission, privacy and
  Hermes suites passed. Frontend build/full suite passed; new key scenarios
  cover explicit opt-in, edit/revoke and retained form state after a failed save.
- Browser QA used only synthetic keys on a local mock management API: keyboard
  switch, save/status round trip, active locale, narrow 390px viewport.

To reproduce the bounded live protocol qualification, build
`./cmd/qualify-external` locally and run it with `--allow-live --max-requests=6`.
It is loopback-only, uses SSH alias netcup-hermes, and keeps the upstream key
inside the existing Hermes container. It must never be shipped as a public proxy.
Live calls consume account quota and require explicit authorization.

Target-version update: the embedded metadata was regenerated from the official
Codex 0.166.0 bundled catalog, preserving the tested Gemini-specific overrides.
Reasoning-effort update items and multi-agent metadata are disabled because v1
only qualifies medium reasoning and ordinary local tool calls.

# CHANGELOG

## v0.4.6-hermes-fallback.1 - Hermes bounded fallback (overlay 0.25.10)

- Added authenticated, opt-in `X-Hermes-Fallback-Policy: v1` for the five exact
  Hermes routes. Marked non-streaming requests require explicit `medium` and
  use existing single-pick auth metadata without changing channel access.
- Disabled executor retries and alternate Antigravity endpoints only for marked
  requests. Ordinary clients retain their current retry behavior.
- Preserved native Gemini `thinkingLevel: medium` after payload rules for the
  selected dynamically registered routes, whose legacy Thinking metadata is
  absent. Conflicting budgets are removed only for this explicit contract.
- Advertised `hermes-fallback-v1` in the authenticated model-catalog response.
  Hermes fails closed on proxies without the capability.
- Full affected handler/executor/auth regression suites passed; actual HTTP
  mocks confirmed a single marked upstream attempt and preserved Luna medium
  plus completed tool history. A local Linux AMD64 build passed (Go 1.27.2,
  CGO disabled). Release retains the v0.4.6 SQLite runtime and existing panel;
  it requires a pinned binary/image and private rollback backup. No credential
  change, migration or paid generation request is needed for deployment.

## v0.4.6 - OAuth Identity Fingerprints and Updater Diagnostics - 2026-06-25

### Highlights

- added account-level OAuth identity fingerprint learning and detail APIs for Claude, Codex, Gemini, and Kimi-compatible runtime flows
- added Codex OAuth client admission presets so OAuth auth files can restrict upstream access to recognized client identities
- added Claude OAuth health tracking and quota reconciliation helpers for clearer auth-file status reporting
- made updater sidecar token configuration explicit and returned health diagnostics instead of a generic unavailable state
- added quota status clear endpoints for auth-file quota and cooldown recovery workflows
- normalized OpenAI-compatible chat tool-call history and routed OpenCode Go tool-output images through vision fallback handling
- made issue triage automation command-driven for safer maintainer control

### Compatibility and upgrade notes

- direct Docker Compose deployments must provide a non-empty `CLIRELAY_UPDATER_TOKEN` shared by the API and updater sidecar
- existing identity-fingerprint configuration remains compatible; learned account records are added through runtime observation
- Codex OAuth admission uses fixed named presets rather than user-supplied matching rules
- the quota status clear endpoint is intended for management recovery flows and does not change normal quota accounting

### Verification

- `rtk go test ./...`
- `rtk git diff --check`
- PR checks for the merged `dev` pull requests

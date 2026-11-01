# Image generation — public-api.2 candidate

Status: implemented and tested locally; not deployed. Existing production remains
public-api.1 until the separate release/data authorization is given.

## Confirmed baseline

The old Hermes uses its installed Antigravity image adapter, whose SHA matches
the repository. It generates with `gemini-3.1-flash-image` (Nano Banana 2), 1K,
through private Chat Completions with image/text modalities. A recent successful
image record used a different channel from the laptop key; that does **not**
prove image quota on the laptop's selected channel.

The laptop currently has no CliRelay image-generation tool configured. The first
public release intentionally exposes only text Responses and catalogs; the
current laptop key allows only the text model. This extension reuses the
Hermes/CliRelay route and does not require another OpenAI credential.

## Candidate contract

- Publish only `POST /v1/images/generations`, rewritten to
  `/external/v1/images/generations`. Generic Chat Completions, edits, uploads,
  OAuth and management remain closed.
- Require the existing external opt-in and explicit `gemini-3.1-flash-image`
  permission. Add only that model to the existing laptop profile; preserve
  the chosen account, all prior limits, key ID/value and durable counters.
  No implicit fallback to the Hermes key or another Google account.
- Accept a text prompt up to 16000 UTF-8 bytes, optional aspect `1:1`, `16:9`
  or `9:16`, `n=1`, and b64_json output. Fixed 1K resolution. No user URLs,
  references, arbitrary messages or tools. Generation only in this release.
- The adapter constructs the working Hermes-style payload in process and uses
  the native auth/execution manager. Existing private handlers are unchanged.
- Images and text share the stable-key RPM/day admission and the two-slot
  external pool. Image deadline 300s; response bounded to a 20 MiB raster.
  External prompts/images/credentials are excluded from detailed logs.
- `/v1/models` reports only explicitly allowed public models; Codex's reasoner
  catalog still contains only Gemini 3.8 Flash. The image model is a tool backend.

## Client

The dependency-free Node stdio MCP module exposes one `imagegen` tool. It reads
the already selected CliRelay credential from the private client config, only
calls the fixed HTTPS image endpoint, and saves new files under
`~/Pictures/CliRelay` (directory 0700, images 0600, exclusive filenames).
It accepts neither caller-controlled network addresses nor output paths.
Cancellation reaches the API; errors never forward provider response bodies.
No HTTP listener, SSH dependency, OpenAI key, Python or Xcode installation.

Results contain a local image path/Markdown, not a base64 image in model history:
the existing text-only Responses contract is preserved. Generated PNG/JPEG/WebP
can be rendered by the desktop client from the local file.

`install-images-macos.mjs` adds only the narrow MCP definition after native config
validation, with a private local backup. It preserves the existing provider and
credential. The owner's existing choice to keep the API key in config 0600 is
unchanged. The MCP tool timeout is 330s.

## Local evidence

- Full Go suite passed (73 package test results); Linux/amd64 build, backend
  structure guard and Caddy validation passed. Existing size warnings remain.
- Go image request/response guards and actual ingress → auth manager → fake
  executor tests passed: private/chat-only keys denied, wrong models/extra fields
  rejected, selected channel/single-attempt metadata retained, durable quota
  enforced before another upstream call, private Hermes catalog still works.
- Five Node tests passed: fixed endpoint and private artifact, config permission
  and argument restrictions, malformed/remote image data and symlink rejection,
  cancellation/no retries/secret suppression, MCP framing/discovery/text result.
- Official Codex CLI 0.166.0 `app-server` discovered `clirelay_images.imagegen`
  in an isolated local thread. No live model/image request was made by that probe.

## Release and recovery

Requires separate COMMIT/PUBLISH, DEPLOY (CliRelay + graceful Caddy reload and
the laptop's MCP definition), PROD_DATA (fresh closed SQLite/config backup and
append one image-model permission to the laptop profile). No Hermes, staging,
Xray or updater restart. No DNS change is needed.

Before replacing CliRelay compare all four effective mounts, loopback ports and
private networks with the baseline; use the corrected absolute-bind overlay.
Check old Hermes again before publishing the image route. Make one bounded
synthetic image test on the selected laptop account. If its image quota/access
is unavailable, fail explicitly and do not change accounts or expose the old key.
Then qualify the actual laptop's tool selection, artifact and next text turn.
Existing conversation contents are not modified or messaged by the release.

For rollback remove/close the image route first, restore the previous pinned
image/Caddy and the laptop's previous MCP config. Remove only the newly added
image model from the laptop profile. Keep counters/history, generated user
images and both original OAuth accounts. Do not restore a stale full database.

References: [Codex MCP configuration](https://learn.chatgpt.com/docs/extend/mcp),
[MCP tools](https://modelcontextprotocol.io/specification/2025-06-18/server/tools),
[stdio transport](https://modelcontextprotocol.io/specification/2025-06-18/basic/transports),
working Hermes adapter `server-extensions/hermes-plugins/image_gen/antigravity`.

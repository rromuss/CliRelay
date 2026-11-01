# Image generation — public-api.2

Status: committed, published, deployed and verified by the actual target-Mac CLI
and owner desktop-image acceptance. Separate release/data authorization was
explicitly received before any production or laptop mutation.

## Confirmed baseline

The old Hermes uses its installed Antigravity image adapter, whose SHA matches
the repository. It generates with `gemini-3.1-flash-image` (Nano Banana 2), 1K,
through private Chat Completions with image/text modalities. At baseline a
successful private image used another channel, so laptop-account image access
was verified separately during this release rather than assumed.

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

## Live evidence — 2026-11-01

- Backend commit `13982f27`, version `v0.4.6-public-api.2`, pinned image
  `clirelay-hermes:public-v2-13982f27`; image ID
  `sha256:452323a4655f30cb986081c453b4a9bee75938b4619b300275f60d1a1036f75f`.
  The prior panel SHA `c1da39079aca34a1889e937199955e156d4ee859` is unchanged.
- Consistent SQLite/config backup kept under root only on the VPS, integrity
  checked, 0700/0600. All four real bind mounts, loopback ports and private
  networks were compared before/after; no PWD-dependent empty mounts occurred.
- Old Hermes private catalog/capabilities/config/key fingerprints matched and
  short inference passed before and after. Hermes, staging, Xray and Caddy
  container start times/restart counts are unchanged; Caddy used graceful reload.
- Only the existing laptop profile received the image model. Key value, stable
  ID, external flags, selected account, 10 RPM/300 UTC-day/2-concurrent limits
  and counters were preserved. The new image route rejects the old internal
  key (403) and `n=2` (400); API catalog has two allowed models, reasoner
  catalog still has only Gemini 3.8.
- MCP installed on the target Mac with a private config backup. The real
  bundled CLI `0.166.0-alpha.5.1` called the installed image tool and generated
  one real strawberry image through the public HTTPS route: JPEG 1024x1024,
  616501 bytes, file mode 0600. Its final response contained the returned
  absolute Markdown image path. A following text turn recalled the test marker.
- The first CLI probe was denied before the API because `codex exec` used
  approval=never. The revised disposable test config approves only the explicitly
  authorized, one-operation-bounded imagegen tool. The actual desktop thread
  retains on-request approvals; no permanent approval rules were weakened.
  After the denied call the model invented an image link; no artifact existed
  and the test rejected it. MCP guidance now explicitly forbids such claims.
- The wrapper and server metadata both confirm exactly one successful image
  operation on the laptop's selected channel. Input/output/detail blobs are
  empty and the stored usage label is not a credential. No automatic retries,
  account fallback, extra image generations or OpenAI image API calls were used.
- A copy of this synthetic fruit fixture was retrieved only for visual review.
  Databases, real client configs, OAuth and personal photos were not copied
  between machines for that review. No real client configs or images were
  added to Git.
- Owner desktop acceptance completed: after restarting ChatGPT, an ordinary
  Russian request to generate a sliced strawberry returned an actual image in
  the Gemini chat. This confirms the natural-language UI flow, not only the
  explicitly directed CLI tool test. Further owner-initiated generations are
  normal use and are separate from the agent's one-image control budget.

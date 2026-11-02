# Gemini vision, built-in browser and local search — v0.4.6-public-api.3

## Source and release boundary

This extends the SQLite-compatible `hermes-stable` source after `eacf93ab`.
The external API, account, keys, model allowlists, RPM/day/concurrency counters,
ingress routes and private Hermes settings are unchanged. No schema migration,
new public route or Caddy change is required. Production deployment and the
client installation require the separate release approval from the agreed plan.

## Protocol

- The Gemini catalog declares `text` and `image` input. Responses messages and
  function/custom-tool results accept inline PNG/JPEG/WebP data URLs with strict
  base64, MIME and signature checks; the entire decoded JSON request stays
  limited to 8 MiB. Image URLs, local paths, file IDs and unsupported part types
  are not resolved or downloaded. No server-side image resizing is added.
- Array tool results retain text and ordering. Each screenshot stays inside its
  matching `functionResponse.parts[].inlineData`, referenced once by a unique
  `displayName` from the ordered structured result. Call IDs, deterministic tool
  aliases and previous string-result behavior are preserved. See Google's
  [multimodal function-response contract](https://ai.google.dev/gemini-api/docs/generate-content/function-calling#multimodal_function_responses).
- `POST /v1/images/generations` additionally accepts `reference_images`: an
  optional array of up to 14 inline raster data URLs within the same 8 MiB body.
  It still produces one 1K image with the existing model, aspect ratios,
  admission rules and response format. No generic chat or URL-fetch API is added.

## Client tools

`clirelay_images.imagegen` adds optional `reference_ids`. To prepare an original,
the model invokes `prepare-reference.mjs` through Codex's normal execution tool
and filesystem permissions. The preparation command is included in the tool's
description using the actual managed Node and installed helper paths. The MCP
accepts only opaque IDs and reads staged copies or its own generated outputs.
Each successful generation/edit returns a reusable `reference_id`, file path
and Markdown. It never overwrites an original or an earlier output.

Staged copies and metadata live in `/private/tmp/clirelay-image-inputs-<uid>`
(0700, files 0600). Calls lazily remove expired staged data after 24 hours; no
daemon is installed. Generated images in `~/Pictures/CliRelay` are not deleted
when their temporary reference metadata expires. References reject traversal,
symlinks, altered files and expired IDs. Combined oversized edits fail before
calling the API. Failed generations are not automatically retried.

`clirelay_search.web_search` is a separate local stdio MCP with only a text
search operation. It uses DDGS 9.16.1 and fully pinned, SHA-256-checked binary
dependencies in a dedicated managed-Python venv. Default: 5 results, maximum:
10. DuckDuckGo is attempted first, then Bing once. Backend work runs in child
processes killed on cancellation/deadline, with a 30-second total budget.
Failures explicitly request built-in-browser fallback; raw provider errors and
query logs are not emitted. Full pages are opened with the built-in browser;
citations must refer to pages actually read. See [DDGS](https://github.com/deedy5/ddgs).

Codex 0.166 includes an eager unnamed hosted `web_search` descriptor when the
root search setting is absent. The external adapter omits that descriptor only
when the concrete `mcp__clirelay_search.web_search` function is also declared.
Only the real client-executed MCP function reaches Gemini; hosted search still
fails explicitly without that executor. The user's removed root setting remains
absent, and no OpenAI-hosted search operation is simulated or forwarded.

The built-in browser uses the existing native CUA tool. No extra browser MCP,
full CDP, global macOS Accessibility permission or automatic site approval is
installed. Native site/sensitive-action confirmations remain in force. See
[OpenAI browser documentation](https://learn.chatgpt.com/docs/browser) and
[MCP configuration](https://learn.chatgpt.com/docs/extend/mcp).

## Installation and rollback

After release approval, feed `install-multimodal-macos.mjs` a JSON object with
`version: "v0.4.6-public-api.3"` and `files` containing the reviewed text of
`clirelay-images.mjs`, `image-references.mjs`, `prepare-reference.mjs`,
`ddgs-search.py`, and `ddgs-requirements.lock`. Run it with the existing managed
Node on the client; never put the credential in this payload or an SSH argument.

The installer creates versioned tool directories, installs binary wheels with
`--require-hashes`, discovers both tools without model calls, and validates a
candidate config with the installed Codex. Only then does it back up the private
config on the client and atomically update the two MCP definitions. Provider
credentials, the removed `web_search="disabled"` setting, other MCPs and existing
tool approvals are preserved. System Python/Xcode and global runtime packages
are not changed. The legacy single-file installer rejects v1.1 source.

Restore the recorded private client config backup to roll back MCP selection;
old tool sources remain available. For the server, restore the preceding pinned
image, preserving the current SQLite data and counters. Do not restore an old
database over new history. Restart only the client and CliRelay; do not restart
Hermes, staging or Xray. Invalidate only the CliRelay provider's catalog cache.

## Verification required before completion

Local gates cover inline image validation, replay, full Antigravity tool-result
conversion, legacy strings, reference authorization/expiry/original preservation,
search framing/fallback/deadline, client config preservation, public access and
quota regressions. Run the affected tests, complete Go suite, structure guard
and Linux build before publishing.

Live gates after approval: visual OCR without tool hints; two-image comparison
and follow-up; real input, button/link click, navigation, scroll and repeated
screenshots in the built-in browser; two bounded image edits (original, then
previous result); separate search, source opening and browser fallback; existing
text/patch/generation and old-Hermes contract checks; safe-log verification.
GUI acceptance on the actual client is required. Local tests alone do not prove
that the installed model can operate the browser or analyze a real attachment.

Preparation evidence (2026-11-02): the complete Go suite passed for 74 packages,
11 Node tests and 6 search tests passed, the backend structure guard passed
(two existing file-size warnings), and the Linux/amd64 server build passed.
Installing the hash-locked DDGS environment succeeded; one public control query
returned three DuckDuckGo results. SSH verified the actual client as Codex
0.166.0-alpha.5.1, Node 24.22.1 and managed Python 3.12.14. The removed hosted
search setting was absent. At that preparation point production still used v2;
the separately approved deployment and the live evidence follow below.

## Approved deployment and automated live evidence

The deployed backend is `f932be33`, image
`clirelay-hermes:public-v3-f932be33` with ID
`sha256:4c01bf1b26bbf84abf77556d3e1d94df4666c837671ec2cc5b3bbbf518a70059`.
The panel remains pinned to `c1da3907`. The client MCP clarification from
`1f966c5a` also places `reference_id` in text content so history does not depend
on a client's treatment of `structuredContent`.

Native Codex 0.166 on the actual client passed OCR with no tool shortcuts,
comparison of two images, image-history replay, and an actual apply_patch. It
prepared the synthetic original through the ordinary execution helper and made
exactly two bounded image edits: original to red background, then the first
result's ID to green background. Real private files and Markdown were produced;
the original hash was unchanged. A separate vision-only turn confirmed those
two background colors. The model called the separate search MCP and returned
real links.

A separate temporary screenshot fixture proved that the live Antigravity path
can read an image nested in a function result and preserve both surrounding
text markers. It allowed only that fixture tool, with no shell/file shortcuts
and no further generation calls. The fixture is not a new browser integration.

Server-side aggregate checks found exactly two successful image operations and
zero captured bytes of external input/output/detail content or credential
labels. All key/profile rows remained unchanged. The old private Hermes
catalog, capabilities, key/config fingerprints and synthetic inference matched
the pre-release baseline. Hermes, staging, Xray and Caddy were not restarted.
Backups and their fingerprints stay closed on the VPS; the private client
config backups stay on the client. No user records were copied into this repo.

`qualify-multimodal-macos.mjs` reproduces the bounded native client checks;
`qualify-tool-images-macos.mjs` adds the nested screenshot check without image
generation. Do not rerun a two-edit qualification after partial success without
accounting for already consumed image calls. The original screenshot/nonce
fixtures and private test profiles are local to the client.

The owner confirmed that the old Telegram-Hermes works after deployment.
Desktop GUI acceptance completed on 2026-11-02 in a new Gemini chat on the
actual client: image input/analysis worked, the built-in browser completed the
form/button/link/scroll workflow and reported the matching per-run page marker,
and search found documentation and opened a source page. These are the owner's
desktop confirmations in addition to the automated checks above. No additional
image generation was requested for this acceptance. CLI browser control is not
claimed; the temporary loopback acceptance pages can be stopped.

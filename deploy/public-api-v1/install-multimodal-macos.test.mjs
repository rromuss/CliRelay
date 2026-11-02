import test from 'node:test';
import assert from 'node:assert/strict';
import { updateClientConfig, isCliRelayOnlyCatalog } from './install-multimodal-macos.mjs';

test('client update preserves provider, removed hosted-search setting, other MCPs and image approvals', () => {
  const provider = '[model_providers.clirelay-antigravity]\nbase_url = "https://cliprelay.romus.pro:8443/v1"\nexperimental_bearer_token = "fixture-not-a-live-secret"\n';
  const original = `model="gemini-3.8-flash-tiered"\n${provider}[mcp_servers.other]\ncommand="keep-me"\n[mcp_servers.clirelay_images]\ncommand="old-node"\nargs=["old-server"]\nenabled=true\n[mcp_servers.clirelay_images.tools.imagegen]\napproval_mode="approve"\n`;
  const options = { node: '/managed/node', imageModule: '/images/releases/new/server.mjs', python: '/search/releases/new/.venv/bin/python3', searchModule: '/search/releases/new/search.py' };
  const result = updateClientConfig(original, options);
  assert.ok(result.includes(provider)); assert.ok(result.includes('[mcp_servers.other]\ncommand="keep-me"'));
  assert.ok(result.includes('[mcp_servers.clirelay_images.tools.imagegen]\napproval_mode="approve"'));
  assert.ok(result.includes('args = ["/images/releases/new/server.mjs"]'));
  assert.ok(result.includes('[mcp_servers.clirelay_search]')); assert.ok(!result.includes('web_search='));
  assert.equal(updateClientConfig(result, options), result);
});

test('cache invalidation is limited to the existing single-model CliRelay catalog', () => {
  assert.equal(isCliRelayOnlyCatalog(JSON.stringify({ models: [{ slug: 'gemini-3.8-flash-tiered' }] })), true);
  for (const raw of ['invalid', '{}', JSON.stringify({ models: [{ slug: 'gpt-fixture' }] }), JSON.stringify({ models: [{ slug: 'gemini-3.8-flash-tiered' }, { slug: 'gpt-fixture' }] })]) assert.equal(isCliRelayOnlyCatalog(raw), false);
});

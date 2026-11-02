import asyncio
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import unittest

FILE = Path(__file__).with_name('ddgs-search.py')
SPEC = importlib.util.spec_from_file_location('ddgs_search', FILE)
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)
RESULT = {'title': 'Official source', 'href': 'https://example.com/source', 'body': 'Snippet'}


class SearchTests(unittest.IsolatedAsyncioTestCase):
    async def test_success_only_calls_primary_and_defaults_to_five(self):
        calls = []
        async def backend(query, limit, name):
            calls.append((query, limit, name))
            return [RESULT]
        result = await MODULE.search({'query': '  current info  '}, backend)
        self.assertEqual(calls, [('current info', 5, 'duckduckgo')])
        self.assertEqual(result['results'][0]['url'], RESULT['href'])

    async def test_one_bing_fallback_then_browser_without_raw_errors(self):
        calls = []
        async def backend(query, limit, name):
            calls.append(name)
            if name == 'duckduckgo':
                raise TimeoutError('upstream private details')
            return [RESULT]
        self.assertEqual((await MODULE.search({'query': 'x', 'max_results': 10}, backend))['backend'], 'bing')
        self.assertEqual(calls, ['duckduckgo', 'bing'])
        async def empty(*_args):
            return []
        result = await MODULE.search({'query': 'x'}, empty)
        self.assertEqual(result['fallback'], 'built-in-browser')
        self.assertNotIn('private', json.dumps(result))

    async def test_invalid_parameters_never_search(self):
        async def backend(*_args):
            self.fail('invalid request reached backend')
        for args in [{}, {'query': ''}, {'query': 'x', 'max_results': True}, {'query': 'x', 'max_results': 11}, {'query': 'x', 'url': 'https://example.com'}, {'query': 'x' * 2001}]:
            with self.assertRaises(ValueError):
                await MODULE.search(args, backend)

    async def test_deadline_cancels_backend(self):
        cancelled = asyncio.Event()
        async def backend(*_args):
            try:
                await asyncio.sleep(10)
            finally:
                cancelled.set()
        result = await MODULE.search({'query': 'x'}, backend, budget=0.02)
        self.assertTrue(cancelled.is_set())
        self.assertEqual(result['fallback'], 'built-in-browser')

    def test_result_bounds_and_urls(self):
        unsafe = [dict(RESULT, href=url) for url in ['file:///private/file', 'https://user:password@example.com', 'http://127.0.0.1', 'http://[::1]', 'http://service.local']]
        results = MODULE.clean_results(unsafe + [RESULT, RESULT, dict(RESULT, href='https://example.org', body='x' * 5000)], 5)
        self.assertEqual(len(results), 2)
        self.assertEqual(len(results[1]['snippet']), 1200)

    def test_stdio_discovers_search_without_importing_ddgs_or_using_network(self):
        messages = [{'jsonrpc': '2.0', 'id': 0, 'method': 'server/discover'}, {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {'protocolVersion': '2025-06-18'}}, {'jsonrpc': '2.0', 'id': 2, 'method': 'tools/list'}]
        result = subprocess.run([sys.executable, str(FILE)], input='\n'.join(map(json.dumps, messages)) + '\n', text=True, capture_output=True, timeout=5, check=True)
        responses = [json.loads(line) for line in result.stdout.splitlines()]
        self.assertEqual(responses[0]['error']['code'], -32601)
        self.assertEqual(responses[2]['result']['tools'][0]['name'], 'web_search')
        self.assertEqual(result.stderr, '')


if __name__ == '__main__':
    unittest.main()

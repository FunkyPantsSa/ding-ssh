#!/usr/bin/env python3
"""Explicit, isolated-instance MCP integration smoke test.

Never use this against a user's workspace: --discovery must point to a test
configuration under DING_SSH_DEV_CONFIG_DIR. Only fixed local fixture commands
are executed. No saved remote servers are accessed; tokens are never printed.
"""
import argparse
import json
from pathlib import Path
import urllib.error
import urllib.request


class Smoke:
    def __init__(self, discovery):
        info = json.loads(Path(discovery).read_text())
        self.base = 'http://127.0.0.1:' + str(info['port'])
        self.token = info['token']
        self.session = None
        self.seq = 0
        self.results = []
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(self, path, body=None, headers=None, auth=True):
        h = {'Accept': 'application/json, text/event-stream'}
        if auth:
            h['Authorization'] = 'Bearer ' + self.token
        if self.session:
            h['Mcp-Session-Id'] = self.session
            h['MCP-Protocol-Version'] = '2025-06-18'
        h.update(headers or {})
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            h['Content-Type'] = 'application/json'
        request = urllib.request.Request(self.base + path, data=data, headers=h)
        with self.http.open(request, timeout=45) as response:
            if response.headers.get('Mcp-Session-Id'):
                self.session = response.headers['Mcp-Session-Id']
            raw = response.read()
            return json.loads(raw) if raw else None

    def rpc(self, method, params=None):
        self.seq += 1
        result = self.request('/mcp', {'jsonrpc': '2.0', 'id': self.seq, 'method': method, 'params': params or {}})
        if result.get('error'):
            raise AssertionError('RPC failure: ' + str(result['error'].get('code')))
        return result['result']

    def tool(self, name, args=None, denied=False):
        result = self.rpc('tools/call', {'name': name, 'arguments': args or {}})
        if denied:
            assert result.get('isError'), name + ' unexpectedly accepted'
            return {'denied': True}
        assert not result.get('isError'), name + ' returned error'
        return result.get('structuredContent') or json.loads(result['content'][0]['text'])

    def expect_status(self, path, code, **kwargs):
        try:
            self.request(path, **kwargs)
        except urllib.error.HTTPError as err:
            assert err.code == code, (err.code, code)
            return {'httpStatus': code}
        raise AssertionError('request unexpectedly accepted')

    def check(self, label, fn):
        result = fn()
        self.results.append({'check': label, 'passed': True, 'detail': result})
        print('PASS', label, flush=True)

    def run(self):
        self.check('Missing authentication rejected', lambda: self.expect_status('/v1/tools', 401, auth=False))
        self.check('Query authentication rejected', lambda: self.expect_status('/v1/tools?token=' + self.token, 401, auth=False))
        self.check('Cross-origin request rejected', lambda: self.expect_status('/v1/tools', 403, headers={'Origin': 'https://untrusted.invalid'}))
        self.check('MCP initialization', lambda: self.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {}, 'clientInfo': {'name': 'isolated-security-smoke', 'version': '1'}})['protocolVersion'])
        self.request('/mcp', {'jsonrpc': '2.0', 'method': 'notifications/initialized'})
        def catalog():
            names = {t['name'] for t in self.rpc('tools/list')['tools']}
            expected = {'exec.start', 'exec.plan', 'exec.batch', 'jobs.wait', 'sftp.patch.preview', 'sftp.patch', 'services.restart', 'sessions.connect', 'servers.search'}
            assert expected <= names, expected - names
            assert names == {t['name'] for t in self.request('/v1/tools')['tools']}
            return {'tools': len(names), 'httpMcpParity': True}
        self.check('Complete catalog and HTTP parity', catalog)
        self.check('Resource templates available', lambda: {'templates': len(self.rpc('resources/templates/list')['resourceTemplates'])})
        self.check('MCP cannot elevate privileges', lambda: self.tool('confirm.prepare', {'action': 'capability.enable', 'args': {'capability': 'eval'}}, denied=True))
        self.check('Settings cannot change debug policy', lambda: self.tool('update_settings', {'settings': {'Debug': {'allowEval': True}}}, denied=True))
        self.check('UI store cannot change debug policy', lambda: self.tool('ui.storePatch', {'name': 'settings', 'patch': {'debug': {'allowEval': True}}}, denied=True))
        def independent_exec():
            args = {'local': True, 'command': "printf 'isolated-smoke-stdout'; printf 'isolated-smoke-stderr' >&2; exit 7", 'idempotencyKey': 'isolated-smoke-exec-v1'}
            job = self.tool('exec.start', args)
            again = self.tool('exec.start', args)
            assert job['jobId'] == again['jobId']
            finished = self.tool('jobs.wait', {'jobId': job['jobId'], 'timeoutMs': 5000})
            output = self.tool('jobs.output', {'jobId': job['jobId']})
            combined = json.dumps(output)
            assert 'isolated-smoke-stdout' in combined and 'isolated-smoke-stderr' in combined
            assert finished['state'] == 'failed' and finished['exitCode'] == 7
            return {'state': finished['state'], 'exitCode': finished['exitCode'], 'idempotent': True, 'stdoutStderr': True}
        self.check('Real local exec streams exit code and idempotency', independent_exec)
        def cancel():
            job = self.tool('exec.start', {'local': True, 'command': 'sleep 30', 'timeoutMs': 40000})
            result = self.tool('jobs.cancel', {'jobId': job['jobId']})
            final = self.tool('jobs.wait', {'jobId': job['jobId'], 'timeoutMs': 5000})
            assert final['state'] in ('cancelled', 'unknown'), final['state']
            return {'requested': bool(result), 'finalState': final['state']}
        self.check('Real local task cancellation', cancel)
        self.check('Frontend mounted without console errors', lambda: self.frontend())
        return self.results

    def frontend(self):
        self.tool('ui.navigate', {'view': 'settings', 'section': 'debug'})
        result = self.tool('ui.query', {'selector': '[aria-label="客户端名称"]'})
        assert result.get('count', 0) == 1
        entries = self.tool('ui.console', {'limit': 100}).get('entries', [])
        assert not any(x.get('level') == 'error' for x in entries)
        return {'clientManagementMounted': True, 'consoleErrors': 0}


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('--discovery', required=True)
    parser.add_argument('--report', required=True)
    options = parser.parse_args()
    smoke = Smoke(options.discovery)
    results = smoke.run()
    Path(options.report).write_text(json.dumps({'passed': len(results), 'checks': results}, indent=2, ensure_ascii=False))

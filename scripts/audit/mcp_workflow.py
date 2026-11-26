#!/usr/bin/env python3
"""Exercise the semantic MCP workflow against an isolated, authorized instance.

Requires local execution/connect/disconnect capabilities. For unattended close
verification the test profile must have close-tab confirmation disabled by its
owner. Never connects saved servers, changes permissions, or records tokens.
"""
import argparse
import json
from pathlib import Path
import sys
import tempfile
import time
import urllib.request

from live_smoke import Audit


class Workflow(Audit):
    def request(self, path, body=None, auth=True, method=None):
        headers = {'Accept': 'application/json, text/event-stream',
                   'X-Ding-SSH-Tool-Profile': 'operations'}
        if self.session:
            headers['Mcp-Session-Id'] = self.session
            headers['MCP-Protocol-Version'] = '2025-06-18'
        if auth:
            headers['Authorization'] = 'Bearer ' + self.token
        data = None
        if body is not None:
            data = json.dumps(body).encode('utf-8')
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        with self.http.open(req, timeout=40) as response:
            self.session = response.headers.get('Mcp-Session-Id') or self.session
            raw = response.read()
            return json.loads(raw) if raw else {}

    def require(self, condition, detail):
        if not condition:
            raise AssertionError(str(detail)[:1200])
        return detail

    def error(self, tool, args, code):
        result = self.rpc('tools/call', {'name': tool, 'arguments': args})
        meta = result.get('_meta', {})
        self.require(result.get('isError') and meta.get('errorCode') == code, result)
        return {'errorCode': meta['errorCode'], 'nextSteps': meta.get('nextSteps', [])}

    def run(self):
        self.rpc('initialize', {'protocolVersion': '2025-06-18', 'capabilities': {},
                               'clientInfo': {'name': 'ding-workflow-audit', 'version': '1'}})
        self.request('/mcp', {'jsonrpc': '2.0', 'method': 'notifications/initialized'})
        def catalog():
            mcp_tools = self.rpc('tools/list')['tools']
            mcp_by_name = {x['name']: x for x in mcp_tools}
            names = set(mcp_by_name)
            self.require({'app.preflight', 'exec.start', 'sessions.close'} <= names, sorted(names))
            self.require('ui.click' not in names and 'open_tab' not in names, sorted(names))
            mcp_meta = mcp_by_name['exec.start'].get('_meta', {})
            expected = {('local=true', ('exec.local',)),
                        ('local omitted or false (sessionId targets SSH)', ('terminal.input',))}
            actual = {(x.get('when'), tuple(x.get('requires', [])))
                      for x in mcp_meta.get('conditionalCapabilities', [])}
            self.require(mcp_meta.get('capabilityMode') == 'conditional' and actual == expected, mcp_meta)
            http_tools = self.request('/v1/tools')['tools']
            http_by_name = {x['name']: x for x in http_tools}
            http = set(http_by_name)
            self.require(http == names, 'HTTP/MCP profile mismatch')
            http_exec = http_by_name['exec.start']
            self.require(http_exec.get('capabilityMode') == 'conditional'
                         and http_exec.get('conditionalCapabilities') == mcp_meta['conditionalCapabilities'], http_exec)
            return {'profile': 'operations', 'tools': len(names), 'httpMcpParity': True,
                    'conditionalExecCapabilities': True}
        self.check('Operations profile is concise and coherent', catalog)
        self.check('Hidden debug tools remain discoverable', lambda: self.require(
            self.tool('tools.search', {'query': 'ui.query', 'profile': 'full', 'includeSchema': True})['count'] > 0,
            {'found': True}))
        self.check('Discovering a hidden tool does not grant permission', lambda: self.error(
            'ui.query', {'selector': 'body'}, 'PERMISSION_DENIED'))
        self.check('Health reports frontend readiness and shell', lambda: self.require(
            self.tool('app.health')['readiness']['frontendResponsive'], {'frontendResponsive': True}))
        local = {'local': True, 'command': 'echo MCP_EXEC_OK', 'waitMs': 5000}
        self.check('Preflight permits exact local execution', lambda: self.require(
            self.tool('app.preflight', {'tool': 'exec.start', 'args': local})['allowed'], {'allowed': True}))
        self.check('Invalid platform shell is caught before execution', lambda: self.require(
            not self.tool('app.preflight', {'tool': 'exec.start', 'args': {**local, 'shell': 'not-a-shell'}})['allowed'],
            {'blocked': True}))
        self.check('Invalid wait is rejected before starting', lambda: self.error('exec.start', {**local, 'waitMs': 30001}, 'INVALID_ARGUMENT'))
        self.check('Missing session returns actionable error', lambda: self.error('sessions.reconnect', {'sessionId': 'audit-missing'}, 'NOT_FOUND'))
        def execute():
            args = {**local, 'idempotencyKey': 'audit-' + str(time.time_ns())}
            result = self.tool('exec.start', args)
            self.require(result.get('complete') and result.get('exitCode') == 0 and 'MCP_EXEC_OK' in result.get('stdout', ''), result)
            again = self.tool('exec.start', {**args, 'waitMs': 1000})
            self.require(again['jobId'] == result['jobId'], again)
            return {'complete': True, 'exitCode': 0, 'sameJobOnRetry': True}
        self.check('One-call execution returns output and preserves idempotency', execute)
        def nonzero():
            result = self.tool('exec.start', {'local': True, 'command': 'exit 7', 'waitMs': 5000})
            self.require(result.get('complete') and result['state'] == 'failed' and result.get('exitCode') == 7, result)
            return {'complete': True, 'state': 'failed', 'exitCode': 7}
        self.check('Nonzero exit remains an explicit task result', nonzero)
        if sys.platform == 'win32':
            with tempfile.TemporaryDirectory(prefix='ding-mcp-中文 ') as cwd:
                def unicode():
                    result = self.tool('exec.start', {'local': True, 'shell': 'powershell',
                        'command': "Write-Output $env:DING_AUDIT_VALUE; Write-Output (Get-Location).Path",
                        'cwd': cwd, 'env': {'DING_AUDIT_VALUE': "中文 '$() & unchanged"}, 'waitMs': 5000})
                    self.require(result.get('complete') and result.get('exitCode') == 0 and cwd in result.get('stdout', '')
                                 and "中文 '$() & unchanged" in result.get('stdout', '') and not result.get('stderr'), result)
                    return {'unicode': True, 'cwd': True, 'envLiteral': True, 'stderrClean': True}
                self.check('PowerShell Unicode cwd/env are literal and readable', unicode)
            def wait():
                result = self.tool('exec.start', {'local': True, 'shell': 'powershell',
                    'command': "Start-Sleep -Milliseconds 600; Write-Output 'MCP_LATER'", 'waitMs': 1})
                self.require(result.get('complete') is False and result.get('nextAction'), result)
                final = self.tool('jobs.wait', {'jobId': result['jobId'], 'timeoutMs': 5000, 'includeOutput': True})
                self.require(final.get('complete') and 'MCP_LATER' in final.get('stdout', ''), final)
                return {'resumable': True, 'complete': True}
            self.check('Short wait returns resumable task and later output', wait)
        session_id = None
        try:
            def connect():
                nonlocal session_id
                result = self.tool('sessions.connect', {'local': True, 'waitMs': 10000})
                session_id = result.get('sessionId')
                self.require(session_id and result.get('tabId') == session_id and result.get('matched') and result.get('status') == 'connected', result)
                return {'returnedIds': True, 'ready': True}
            self.check('Connect returns IDs and waits for ready without tab scanning', connect)
            if session_id:
                def disconnect():
                    result = self.tool('sessions.disconnect', {'sessionId': session_id})
                    self.require(result.get('matched') and result.get('backendConnected') is False, result)
                    return {'disconnected': True}
                self.check('Disconnect waits for backend completion', disconnect)
                def reconnect():
                    preflight = self.tool('app.preflight', {'tool': 'sessions.reconnect', 'args': {'sessionId': session_id}})
                    self.require(preflight['allowed'] and 'exec.local' in preflight['requiredCapabilities']
                                 and 'credentials.use' not in preflight['requiredCapabilities'], preflight)
                    result = self.tool('sessions.reconnect', {'sessionId': session_id})
                    self.require(result.get('matched') and result.get('status') == 'connected', result)
                    return {'connected': True, 'credentialPermissionNotRequired': True}
                self.check('Local reconnect works without credential permission', reconnect)
        finally:
            if session_id:
                def close():
                    result = self.tool('sessions.close', {'sessionId': session_id})
                    self.require(result.get('reason') == 'closed' and result.get('backendConnected') is False
                                 and result.get('tabExists') is False, result)
                    status = self.tool('sessions.closeStatus', {'sessionId': session_id})
                    self.require(status.get('reason') == 'closed', status)
                    return {'closed': True, 'noDOMSelectors': True, 'backendClean': True}
                self.check('Close confirms full cleanup through semantic MCP only', close)
        return self.results


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--debug-info', required=True)
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    audit = Workflow(args.debug_info)
    try:
        audit.run()
    finally:
        Path(args.output).write_text(json.dumps({'results': audit.results}, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')
    return int(any(x['status'] == 'FAIL' for x in audit.results))


if __name__ == '__main__':
    sys.exit(main())

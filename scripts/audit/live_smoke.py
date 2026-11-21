#!/usr/bin/env python3
"""Repeatable smoke tests against an explicitly enabled ding-ssh debug server.

Uses only temporary local terminals, restores the active view, never connects
to saved remote servers, and never records debug tokens or saved credentials.
"""
import argparse
import base64
import json
import os
from pathlib import Path
import sys
import time
import urllib.error
import urllib.request


class Audit:
    def __init__(self, discovery):
        info = json.loads(Path(discovery).read_text())
        self.base = f"http://127.0.0.1:{info['port']}"
        self.token = info['token']
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.seq = 0
        self.results = []
        self.created = []

    def request(self, path, body=None, auth=True, method=None):
        headers = {'Accept': 'application/json'}
        if auth:
            headers['Authorization'] = 'Bearer ' + self.token
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers['Content-Type'] = 'application/json'
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        with self.http.open(req, timeout=35) as response:
            return json.load(response)

    def rpc(self, method, params=None):
        self.seq += 1
        reply = self.request('/mcp', {'jsonrpc': '2.0', 'id': self.seq,
                                      'method': method, 'params': params or {}})
        if reply.get('error'):
            raise AssertionError(json.dumps(reply['error'], ensure_ascii=False))
        return reply.get('result', {})

    def tool(self, name, args=None):
        result = self.rpc('tools/call', {'name': name, 'arguments': args or {}})
        if result.get('isError'):
            raise AssertionError(str(result.get('content', []))[:700])
        if 'structuredContent' in result:
            return result['structuredContent']
        content = result.get('content', [])
        text = '\n'.join(x.get('text', '') for x in content)
        try:
            return json.loads(text)
        except (ValueError, TypeError):
            return text

    def check(self, name, fn):
        start = time.monotonic()
        try:
            detail = fn()
            result = {'name': name, 'status': 'PASS', 'detail': detail}
        except Exception as error:
            status = 'SKIP' if name == 'MCP lists resource templates' and '-32601' in str(error) else 'FAIL'
            result = {'name': name, 'status': status, 'detail': str(error)[:900]}
        result['seconds'] = round(time.monotonic() - start, 3)
        self.results.append(result)
        print(result['status'], name, flush=True)
        return result

    def wait(self, fn, seconds=12):
        deadline = time.monotonic() + seconds
        last = None
        while time.monotonic() < deadline:
            last = fn()
            if last:
                return last
            time.sleep(.1)
        raise AssertionError(f'condition not reached within {seconds}s; last={last}')

    def open_local(self):
        before = {t['clientId'] for t in self.request('/v1/state')['frontend']['tabs']}
        self.tool('open_tab', {'local': True})
        def ready():
            tabs = self.request('/v1/state')['frontend']['tabs']
            for tab in tabs:
                if tab['clientId'] not in before:
                    if tab['clientId'] not in self.created:
                        self.created.append(tab['clientId'])
                    if tab['status'] == 'connected':
                        return tab
            return None
        return self.wait(ready)

    def close_created(self):
        for tab in self.created:
            existing = self.request('/v1/state')['frontend']['tabs']
            if not any(t['clientId'] == tab for t in existing):
                continue
            self.tool('close_tab', {'id': tab})
            # Respect the user's close-confirmation preference and click the
            # confirmation for this test's own terminal only.
            query = self.tool('ui.query', {'selector': '[data-close-cancel] + button'})
            if query.get('count', 0):
                self.tool('ui.click', {'selector': '[data-close-cancel] + button'})
            self.wait(lambda: not any(t['clientId'] == tab for t in
                                     self.request('/v1/state')['frontend']['tabs']))
        self.wait(lambda: not any(s.get('id') in self.created or s.get('sessionId') in self.created
                                for s in self.request('/v1/state')['sessions']))
        return {'closed': len(self.created), 'remainingTestSessions': 0}

    def run(self):
        initial = self.request('/v1/state')
        original_view = initial['frontend']['view']
        original_section = self.tool('ui.store', {'name': 'ui'})
        original_sessions = self.tool('ui.store', {'name': 'sessions'}).get('state', {})
        initial_tabs = {t['clientId'] for t in initial['frontend']['tabs']}
        def unauthorized():
            try:
                self.request('/v1/tools', auth=False)
            except urllib.error.HTTPError as error:
                assert error.code == 401, error.code
                return {'httpStatus': 401}
            raise AssertionError('unauthenticated request was accepted')
        self.check('HTTP rejects missing authentication', unauthorized)
        def catalog():
            http = self.request('/v1/tools')
            mcp = self.rpc('tools/list')
            names = [t['name'] for t in http['tools']]
            assert len(names) == len(set(names)) == http['count']
            assert set(names) == {t['name'] for t in mcp['tools']}
            return {'tools': len(names), 'duplicateNames': 0, 'httpMcpParity': True}
        self.check('HTTP and MCP tool catalog parity', catalog)
        for name, args in [('app.health', {}), ('app.permissions', {}),
                           ('settings.schema', {}), ('groups.list', {}),
                           ('servers.list', {'includeSecrets': False}),
                           ('credentials.list', {}), ('tunnels.list', {}),
                           ('hostTrust.list', {}), ('app.commands', {}),
                           ('list_logs', {}), ('read_logs', {'lines': 10}),
                           ('recent_events', {'limit': 5}),
                           ('app.recent_calls', {'limit': 5}), ('sftp.transfers', {})]:
            self.check('Read tool: ' + name,
                       lambda name=name, args=args: {'responseType': type(self.tool(name, args)).__name__})
        self.check('MCP initializes successfully', lambda: {'protocol': self.rpc('initialize', {
            'protocolVersion': '2025-06-18', 'capabilities': {},
            'clientInfo': {'name': 'ding-audit', 'version': '1'}})['protocolVersion']})
        self.check('MCP lists resources', lambda: {'resources': len(self.rpc('resources/list')['resources'])})
        self.check('MCP lists resource templates', lambda: {'templates': len(self.rpc('resources/templates/list')['resourceTemplates'])})

        try:
            for view, selector in [('home', '[data-ui="home"]'),
                                   ('servers', '[data-ui="server-list"]'),
                                   ('tunnel', '[data-ui="tunnel-page"]')]:
                def navigate(view=view, selector=selector):
                    self.tool('ui.navigate', {'view': view})
                    state = self.request('/v1/state')['frontend']
                    assert state['view'] == view, state['view']
                    layout = self.tool('ui.layout')
                    assert layout.get('entries'), layout
                    return {'view': state['view'], 'layoutEntries': len(layout['entries'])}
                self.check('Navigate page: ' + view, navigate)
            for section in ['general', 'hotkeys', 'connectionInfo', 'theme', 'credentials',
                            'security', 'debug', 'logs', 'audit', 'migrate', 'about']:
                def settings(section=section):
                    self.tool('ui.navigate', {'view': 'settings', 'section': section})
                    reply = self.tool('ui.assert', {'selector': f'[data-ui="settings-section-{section}"]',
                                                   'exists': True, 'visible': True})
                    assert reply.get('passed'), {k: reply.get(k) for k in ['passed', 'checks', 'found']}
                    return {'section': section, 'visible': True}
                self.check('Settings section: ' + section, settings)

            first = self.open_local()
            self.check('Real local shell connects', lambda: {
                'kind': first['kind'], 'connected': first['status'] == 'connected',
                'shell': first['name']})
            terminal_id = first['clientId']
            def run_command(command, expected):
                response = self.tool('terminal.run', {'id': terminal_id, 'command': command,
                                      'expect': expected, 'timeoutMs': 8000})
                assert response.get('matched') is True, response
                return {'matched': True, 'reason': response['reason']}
            self.check('Local shell ASCII command and output', lambda: run_command(
                "printf '\\101\\125\\104\\111\\124\\137\\117\\113\\012'", 'AUDIT_OK'))
            self.check('Local shell Unicode command input', lambda: run_command(
                "printf 'é' | od -An -tx1", r'c3\s+a9'))
            self.check('Local shell Chinese command input', lambda: run_command(
                "printf '测试' | od -An -tx1", r'e6\s+b5\s+8b\s+e8\s+af\s+95'))
            def base64_input():
                payload = base64.b64encode(b"printf '\\102\\111\\116\\101\\122\\131\\137\\117\\113\\012'\r").decode()
                self.tool('send_input', {'id': terminal_id, 'base64': payload})
                self.wait(lambda: 'BINARY_OK' in self.tool('read_terminal', {'id': terminal_id, 'from': 'head', 'lines': 2000}).get('text', ''))
                return {'rawBase64Preserved': True}
            self.check('Local shell binary/base64 input', base64_input)
            self.check('Local terminal resize', lambda: self.tool('resize_terminal', {'id': terminal_id, 'cols': 90, 'rows': 25}))
            self.check('Local terminal scroll', lambda: self.tool('scroll_terminal', {'id': terminal_id, 'to': 'bottom'}))
            second = self.open_local()
            def split():
                self.tool('workspace.split', {'sourceId': terminal_id, 'targetId': second['clientId'], 'direction': 'right'})
                assert self.request('/v1/state')['frontend']['splitShown']
                self.tool('workspace.split', {'enabled': False})
                assert not self.request('/v1/state')['frontend']['splitShown']
                return {'splitAndRestore': True}
            self.check('Split and restore two live terminals', split)
            for panel in ['sftp', 'sysinfo', 'transfers', 'hidden']:
                self.check('Switch context panel: ' + panel,
                           lambda panel=panel: self.tool('panels.set', {'panel': panel}))
            def disconnect():
                self.tool('disconnect_session', {'id': terminal_id})
                self.wait(lambda: any(t['clientId'] == terminal_id and t['status'] == 'closed'
                                      for t in self.request('/v1/state')['frontend']['tabs']))
                return {'closed': True}
            self.check('Disconnect a real local session', disconnect)
            def reconnect():
                self.tool('reconnect_session', {'id': terminal_id})
                self.wait(lambda: any(t['clientId'] == terminal_id and t['status'] == 'connected'
                                      for t in self.request('/v1/state')['frontend']['tabs']))
                return run_command("printf '\\122\\105\\103\\117\\116\\116\\105\\103\\124\\137\\117\\113\\012'", 'RECONNECT_OK')
            self.check('Reconnect a real local session and run a command', reconnect)
            def console():
                entries = self.tool('ui.console', {'limit': 100}).get('entries', [])
                errors = [x for x in entries if x.get('level') == 'error']
                assert not errors, str(errors)[:700]
                return {'errors': 0, 'entries': len(entries)}
            self.check('Frontend console has no errors', console)
        finally:
            self.check('Cleanup only audit-created tabs and PTYs', self.close_created)
            self.tool('ui.navigate', {'view': original_view})
            # Restore the settings section as well as the visible page.
            state = original_section.get('state', {}) if isinstance(original_section, dict) else {}
            section = state.get('settingsSection')
            if section:
                self.tool('ui.storePatch', {'name': 'ui', 'patch': {'settingsSection': section}})
            panel_patch = {key: original_sessions[key] for key in ['rightPanel', 'sftpVisible']
                           if key in original_sessions}
            if panel_patch:
                self.tool('ui.storePatch', {'name': 'sessions', 'patch': panel_patch})
            active = original_sessions.get('activeId')
            if active in initial_tabs:
                self.tool('tabs.activate', {'id': active})
                self.tool('ui.navigate', {'view': original_view})
            remaining = {t['clientId'] for t in self.request('/v1/state')['frontend']['tabs']}
            self.check('Original tabs preserved', lambda: self.assert_equal(remaining, initial_tabs))
        return self.results

    @staticmethod
    def assert_equal(actual, expected):
        assert actual == expected, f'expected {expected}, got {actual}'
        return {'preserved': True}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    default = Path.home() / ('Library/Application Support' if sys.platform == 'darwin' else '.config') / 'ding-ssh/debug.json'
    parser.add_argument('--debug-info', default=str(default))
    parser.add_argument('--output', required=True)
    args = parser.parse_args()
    audit = Audit(args.debug_info)
    try:
        audit.run()
    finally:
        Path(args.output).write_text(json.dumps({'results': audit.results}, ensure_ascii=False, indent=2) + '\n')
    return 1 if any(r['status'] == 'FAIL' for r in audit.results) else 0


if __name__ == '__main__':
    sys.exit(main())

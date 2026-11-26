#!/usr/bin/env python3
"""Native Wails MCP observation/action smoke, against an isolated profile only."""

import argparse
import base64
import json
import time
import urllib.error
import urllib.request
from pathlib import Path


PROTOCOL_VERSION = "2025-06-18"


class Client:
    def __init__(self, discovery_path: Path):
        self.info = json.loads(discovery_path.read_text())
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        self.session_id = None
        self.sequence = 0

    def _headers(self):
        headers = {
            "Authorization": "Bearer " + self.info["token"],
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
        }
        if self.session_id:
            headers.update(
                {
                    "Mcp-Session-Id": self.session_id,
                    "MCP-Protocol-Version": PROTOCOL_VERSION,
                }
            )
        return headers

    def _post(self, payload):
        request = urllib.request.Request(
            self.info["url"] + "/mcp",
            data=json.dumps(payload).encode(),
            headers=self._headers(),
        )
        try:
            with self.http.open(request, timeout=35) as response:
                self.session_id = response.headers.get("Mcp-Session-Id") or self.session_id
                return response.status, response.read()
        except urllib.error.HTTPError as error:
            body = error.read().decode("utf-8", "replace")
            raise RuntimeError(f"MCP HTTP {error.code}: {body[:1000]}") from error

    def rpc(self, method, params=None):
        self.sequence += 1
        status, body = self._post(
            {
                "jsonrpc": "2.0",
                "id": self.sequence,
                "method": method,
                "params": params or {},
            }
        )
        if status != 200:
            raise AssertionError(f"{method} returned HTTP {status}, expected 200")
        response = json.loads(body)
        if response.get("error"):
            raise AssertionError(response["error"])
        return response.get("result", {})

    def notification(self, method, params=None):
        status, _ = self._post(
            {"jsonrpc": "2.0", "method": method, "params": params or {}}
        )
        if status != 202:
            raise AssertionError(f"{method} returned HTTP {status}, expected 202")

    def tool(self, name, args=None, image_path=None):
        response = self.rpc(
            "tools/call", {"name": name, "arguments": args or {}}
        )
        if response.get("isError"):
            raise AssertionError(response.get("content"))
        if image_path:
            images = [
                item
                for item in response.get("content", [])
                if item.get("type") == "image"
            ]
            assert images, f"{name}: native image missing"
            image_path = Path(image_path)
            image_path.parent.mkdir(parents=True, exist_ok=True)
            image_path.write_bytes(base64.b64decode(images[0]["data"]))
        if "structuredContent" in response:
            return response["structuredContent"]
        text = "\n".join(
            item.get("text", "")
            for item in response.get("content", [])
            if item.get("type") == "text"
        )
        return json.loads(text)


def one_node(snapshot, predicate, description):
    matches = [node for node in snapshot["nodes"] if predicate(node)]
    assert len(matches) == 1, (
        f"{description}: expected one visible node, found {len(matches)}"
    )
    assert matches[0].get("actionable"), f"{description}: target is not actionable"
    return matches[0]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--discovery", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument(
        "--native-focus-delay-seconds",
        type=float,
        default=10,
        help=(
            "Time to focus the isolated Wails window. Native input never steals "
            "focus; use 0 to skip."
        ),
    )
    args = parser.parse_args()
    if args.native_focus_delay_seconds < 0:
        parser.error("--native-focus-delay-seconds must be nonnegative")

    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    discovery_path = Path(args.discovery).resolve()
    assert ".smoke" in discovery_path.parts, (
        "only an explicit isolated .smoke profile is accepted"
    )

    client = Client(discovery_path)
    checks = []

    def check(name, function):
        started = time.monotonic()
        try:
            detail = function()
            status = "PASS"
        except Exception as error:
            detail = {"error": str(error)[:1000]}
            status = "FAIL"
        checks.append(
            {
                "name": name,
                "status": status,
                "seconds": round(time.monotonic() - started, 3),
                "detail": detail,
            }
        )
        print(status, name, flush=True)

    client.rpc(
        "initialize",
        {
            "protocolVersion": PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {"name": "ding-observability-smoke", "version": "1"},
        },
    )
    assert client.session_id, "initialize did not establish an MCP session"
    client.notification("notifications/initialized")

    def catalog():
        tools = client.rpc("tools/list")["tools"]
        names = {tool["name"] for tool in tools}
        assert len(names) == len(tools), "tool names are not unique"
        required = {
            "app.observe",
            "ui.observe",
            "ui.inspect",
            "ui.act",
            "ui.tree",
            "ui.wait",
            "ui.diagnose",
            "ui.compare",
            "app.diagnostics",
            "app.runtimeCapabilities",
            "verify.run",
            "verify.get",
            "jobs.wait",
            "targets.probe",
        }
        assert required <= names, f"missing tools: {sorted(required - names)}"
        return {"count": len(names), "requiredCount": len(required)}

    check("tool catalog", catalog)

    def tree():
        result = client.tool("ui.tree", {"depth": 3, "limit": 100})
        assert result.get("total", 0) > 0, result
        assert result.get("source") == "public-component-registry", result
        return {"count": result["total"], "source": result["source"]}

    check("production component registry", tree)

    observed = {}

    def observe():
        result = client.tool(
            "app.observe",
            {"ignoreTerminalOutput": True},
            output / "initial.png",
        )
        assert result.get("ready"), result
        assert result.get("capture", {}).get("status") == "captured", result
        assert result["capture"].get("provider") == "wkwebview-snapshot", result
        observed.update(result)
        return {
            "observationId": result["observationId"],
            "capture": result["capture"],
            "nodes": len(result["nodes"]),
            "health": result["health"],
        }

    check("native screenshot and observation", observe)

    def inspect():
        node = next(node for node in observed["nodes"] if node.get("actionable"))
        result = client.tool(
            "ui.inspect",
            {
                "observationId": observed["observationId"],
                "targetRef": node["ref"],
            },
        )
        assert result.get("rect"), result
        return {"role": node.get("role"), "hasClipping": bool(result.get("clipping"))}

    check("stable node inspection", inspect)

    def actionability_and_effect():
        current = client.tool("ui.observe", {"includeImage": False})
        server_node = next(
            node
            for node in current["nodes"]
            if node.get("automationId") == "nav-item-servers" and node.get("actionable")
        )
        ambiguous = client.tool(
            "ui.act",
            {
                "selector": '[data-ui="sidebar"] button',
                "action": "click",
                "includeImage": False,
            },
        )
        assert ambiguous.get("accepted") is False
        assert ambiguous.get("dispatched") is False

        result = client.tool(
            "ui.act",
            {
                "observationId": current["observationId"],
                "targetRef": server_node["ref"],
                "action": "click",
                "wait": {
                    "condition": {"kind": "view", "view": "servers"},
                    "timeoutMs": 3000,
                },
            },
            output / "servers.png",
        )
        assert result.get("accepted") and result.get("dispatched"), result
        assert result.get("effectConfirmed"), result
        return {
            "ambiguousRejected": True,
            "effectConfirmed": result["effectConfirmed"],
            "inputMode": result["inputMode"],
        }

    check("actionability and explicit effect", actionability_and_effect)

    def native_act(snapshot, node, action, wait, artifact_name, params=None):
        tool_args = {
            "observationId": snapshot["observationId"],
            "targetRef": node["ref"],
            "action": action,
            "inputMode": "native-window-events",
            "wait": wait,
            "includeImage": True,
        }
        if params is not None:
            tool_args["params"] = params
        result = client.tool("ui.act", tool_args, output / artifact_name)
        assert result.get("accepted") is True and result.get("dispatched") is True, result
        assert result.get("inputMode") == "native-window-events", result
        assert result.get("retrySafe") is False, result
        assert result.get("effectConfirmed") is True, result
        assert result.get("trustedEvent") == "unverified", result
        capture = result.get("observation", {}).get("capture", {})
        assert capture.get("status") == "captured", capture
        assert capture.get("provider") == "wkwebview-snapshot", capture
        assert capture.get("consistency") == "stable-window", capture
        return result

    def native_interactions():
        capabilities = client.tool("app.runtimeCapabilities")
        native = capabilities.get("input", {}).get("native", {})
        assert native.get("available") is True, native
        assert native.get("provider") == "appkit-nsevent", native
        assert {"click", "type", "key"} <= set(native.get("actions", [])), native

        if args.native_focus_delay_seconds:
            print(
                "Focus the isolated Wails window now; native input begins in "
                f"{args.native_focus_delay_seconds:g}s.",
                flush=True,
            )
            time.sleep(args.native_focus_delay_seconds)

        before = client.tool("ui.observe", {"includeImage": False})
        assert not any(
            node.get("automationId") == "command-palette" for node in before["nodes"]
        ), "command palette must be closed before the smoke"
        trigger = one_node(
            before,
            lambda node: node.get("automationId") == "command-trigger",
            "command palette trigger",
        )
        opened = native_act(
            before,
            trigger,
            "click",
            {
                "condition": {
                    "kind": "element",
                    "selector": '[data-ui="command-palette"]',
                    "visible": True,
                },
                "timeoutMs": 3000,
            },
            "native-command-open.png",
        )

        modal = client.tool("ui.observe", {"includeImage": False})
        search = one_node(
            modal,
            lambda node: node.get("tag") == "input" and node.get("role") == "combobox",
            "command palette search",
        )
        typed = native_act(
            modal,
            search,
            "type",
            {
                "condition": {
                    "kind": "element",
                    "selector": '[data-ui="empty-state"]',
                    "visible": True,
                },
                "timeoutMs": 3000,
            },
            "native-search-input.png",
            {"text": "zzzz_mcp_smoke_no_match_2026"},
        )

        filtered = client.tool("ui.observe", {"includeImage": False})
        search = one_node(
            filtered,
            lambda node: node.get("tag") == "input" and node.get("role") == "combobox",
            "command palette search after typing",
        )
        closed = native_act(
            filtered,
            search,
            "key",
            {
                "condition": {
                    "kind": "element",
                    "selector": '[data-ui="command-palette"]',
                    "exists": False,
                },
                "timeoutMs": 3000,
            },
            "native-command-close.png",
            {"key": "Escape"},
        )
        return {
            "provider": native["provider"],
            "actions": [
                {
                    "action": "click",
                    "operationId": opened["operationId"],
                    "beforeObservationId": before["observationId"],
                    "afterObservationId": opened["observation"]["observationId"],
                    "effectConfirmed": opened["effectConfirmed"],
                    "captureProvider": opened["observation"]["capture"]["provider"],
                    "captureConsistency": opened["observation"]["capture"]["consistency"],
                },
                {
                    "action": "type",
                    "operationId": typed["operationId"],
                    "beforeObservationId": modal["observationId"],
                    "afterObservationId": typed["observation"]["observationId"],
                    "effectConfirmed": typed["effectConfirmed"],
                    "captureProvider": typed["observation"]["capture"]["provider"],
                    "captureConsistency": typed["observation"]["capture"]["consistency"],
                },
                {
                    "action": "key:Escape",
                    "operationId": closed["operationId"],
                    "beforeObservationId": filtered["observationId"],
                    "afterObservationId": closed["observation"]["observationId"],
                    "effectConfirmed": closed["effectConfirmed"],
                    "captureProvider": closed["observation"]["capture"]["provider"],
                    "captureConsistency": closed["observation"]["capture"]["consistency"],
                },
            ],
            "artifacts": [
                "native-command-open.png",
                "native-search-input.png",
                "native-command-close.png",
            ],
            "trustedEvent": "unverified",
            "limitations": native.get("limitations", []),
        }

    check("native click, text input, and Escape effects", native_interactions)

    def comparison():
        baseline = client.tool("ui.observe")
        current = client.tool("ui.observe")
        result = client.tool(
            "ui.compare",
            {
                "baselineId": baseline["observationId"],
                "currentId": current["observationId"],
            },
        )
        assert result["comparable"], result
        return {"comparable": True, "pixels": result.get("pixels")}

    check("same environment comparison", comparison)

    def wait_failure():
        result = client.tool(
            "ui.wait",
            {
                "condition": {
                    "kind": "element",
                    "selector": "[data-does-not-exist]",
                    "exists": True,
                },
                "timeoutMs": 100,
            },
        )
        assert not result["matched"] and result["timedOut"], result
        return {
            "timedOut": True,
            "pending": result["stability"]["pending"],
        }

    check("wait timeout evidence", wait_failure)

    def verify():
        job = client.tool(
            "verify.run",
            {"scenarioId": "desktop.current", "idempotencyKey": "smoke-current-1"},
        )
        state = client.tool(
            "jobs.wait", {"jobId": job["jobId"], "timeoutMs": 15000}
        )
        result = client.tool("verify.get", {"jobId": job["jobId"]})
        raw = "".join(item["text"] for item in result["output"]["chunks"])
        report = json.loads(raw)
        assert report["status"] == "passed", report
        return {"jobState": state["state"], "verification": report}

    check("persisted verify job", verify)

    report = {
        "capturedAt": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
        "environment": "macOS native Wails production build",
        "instanceId": client.info.get("instanceId"),
        "buildId": client.info.get("buildId"),
        "checks": checks,
        "passed": sum(item["status"] == "PASS" for item in checks),
        "failed": sum(item["status"] == "FAIL" for item in checks),
        "limitations": [
            "No saved user remote target was accessed.",
            "Native provider reports trustedEvent=unverified; this smoke confirms provider delivery plus UI state effects, not browser isTrusted semantics.",
            "Windows/Linux native input is not implemented.",
        ],
    }
    (output / "report.json").write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n")
    raise SystemExit(1 if report["failed"] else 0)


if __name__ == "__main__":
    main()

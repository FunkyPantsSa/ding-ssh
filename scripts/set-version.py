#!/usr/bin/env python3
"""Write productVersion into wails.json (required for NSIS / Info.plist)."""
from __future__ import annotations

import json
import os
import pathlib
import sys

version = os.environ.get("WAILS_VERSION") or (sys.argv[1] if len(sys.argv) > 1 else "")
if not version:
    sys.exit("WAILS_VERSION or argv[1] required (x.y.z)")

root = pathlib.Path(__file__).resolve().parent.parent
path = root / "wails.json"
data = json.loads(path.read_text())
info = data.setdefault("info", {})
info["productVersion"] = version
path.write_text(json.dumps(data, indent=2) + "\n")
print(f"wails.json productVersion = {version}")

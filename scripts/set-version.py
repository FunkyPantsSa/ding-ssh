#!/usr/bin/env python3
"""Stamp the native bundle and Windows executable metadata."""
from __future__ import annotations

import json
import os
from pathlib import Path
import plistlib
import re
import sys

version = os.environ.get("WAILS_VERSION") or (sys.argv[1] if len(sys.argv) > 1 else "")
if not re.fullmatch(r"[0-9]+\.[0-9]+\.[0-9]+", version):
    sys.exit("WAILS_VERSION or argv[1] required (x.y.z)")

build = Path(__file__).resolve().parent.parent / "build"
plist_path = build / "darwin" / "Info.plist"
plist = plistlib.loads(plist_path.read_bytes())
plist["CFBundleVersion"] = version
plist["CFBundleShortVersionString"] = version
plist_path.write_bytes(plistlib.dumps(plist, sort_keys=False))

info_path = build / "windows" / "info.json"
info = json.loads(info_path.read_text(encoding="utf-8"))
info["fixed"]["file_version"] = version
info["info"]["0000"]["ProductVersion"] = version
info_path.write_text(json.dumps(info, ensure_ascii=False, indent=4) + "\n", encoding="utf-8")
manifest_path = build / "windows" / "wails.exe.manifest"
manifest = manifest_path.read_text(encoding="utf-8")
manifest, count = re.subn(
    r'(name="com\.funkypantssa\.ding-ssh" version=")[^"]+(")',
    lambda match: f"{match[1]}{version}.0{match[2]}",
    manifest,
)
if count != 1:
    sys.exit("Unexpected application identity in the Windows manifest")
manifest_path.write_text(manifest, encoding="utf-8")
print(f"Native product version = {version}")

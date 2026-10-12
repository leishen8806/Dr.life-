#!/usr/bin/env python3
import pathlib
import re
import sys
import urllib.parse

root = pathlib.Path(__file__).resolve().parents[2]
readme = (root / "README.md").read_text(encoding="utf-8")
missing = []
for match in re.finditer(r"\[[^\]]*\]\((?:<([^>]+)>|([^\s)]+))\)", readme):
    target = urllib.parse.unquote(match.group(1) or match.group(2)).split("#", 1)[0]
    if target.endswith(".md") and not (root / target).is_file():
        missing.append(target)

if missing:
    for target in sorted(set(missing)):
        print(f"missing README document: {target}", file=sys.stderr)
    sys.exit(1)

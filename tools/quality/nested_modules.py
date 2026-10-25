#!/usr/bin/env python3
"""Validate active module discovery and emit the maintained CI matrix.

Git's normal tracked/unignored source boundary excludes historical evidence and
.ctx. Include new untracked modules so candidate checks catch missing entries.
"""
import json
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[2]
expected = json.loads((root / "tools/quality/nested-go-modules.json").read_text())
files = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*go.mod"],
    cwd=root,
).decode().split("\0")
actual = sorted({str(Path(p).parent) for p in files if p and Path(p).name == "go.mod" and Path(p).parent != Path(".")})
if sorted(expected) != actual or len(expected) != len(set(expected)):
    print(f"Nested module CI list drift: expected={expected}, discovered={actual}", file=sys.stderr)
    sys.exit(1)
print(json.dumps(actual, separators=(",", ":")))

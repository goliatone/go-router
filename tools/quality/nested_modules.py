#!/usr/bin/env python3
"""Validate active module discovery and emit the maintained CI matrix.

Use Git's tracked/unignored source boundary, excluding historical evidence and
.ctx. Include new untracked modules so candidate checks catch missing entries.
"""
import argparse
import json
from pathlib import Path
import subprocess
import sys

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--lines", action="store_true", help="emit one module directory per line")
args = parser.parse_args()

root = Path(__file__).resolve().parents[2]
expected = json.loads((root / "tools/quality/nested-go-modules.json").read_text())
files = subprocess.check_output(
    ["git", "ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "*go.mod"],
    cwd=root,
).decode().split("\0")
actual = sorted({
    str(Path(p).parent) for p in files
    if p and Path(p).name == "go.mod" and Path(p).parent != Path(".")
    # evidence/go.mod is intentionally visible to Git only as an archive boundary.
    and Path(p).parts[0] not in {"evidence", ".ctx"}
})
if sorted(expected) != actual or len(expected) != len(set(expected)):
    print(f"Nested module CI list drift: expected={expected}, discovered={actual}", file=sys.stderr)
    sys.exit(1)
if args.lines:
    if actual:
        print("\n".join(actual))
else:
    print(json.dumps(actual, separators=(",", ":")))

#!/usr/bin/env python3
"""Exercise the strict taskfile commands with inert Git/npm/GitHub API and temporary release metadata.

No real Git command, commit, tag, push, network request or npm script runs. Calls
are recorded and every successful-path external boundary is failed in turn.
"""
import json
import os
import runpy
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
HEAD = "a" * 40
REMOTE_HEAD = "b" * 40
TAG = "c" * 40

MOCK = r'''#!/usr/bin/env python3
import json, os, pathlib, subprocess, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
root = pathlib.Path(os.environ["FIXTURE"])
log = root / "calls.jsonl"
rows = log.read_text().splitlines() if log.exists() else []
with log.open("a") as f:
    f.write(json.dumps([name, *args]) + "\n")
if len(rows) + 1 == int(os.environ.get("FAIL_AT", "0")):
    sys.exit(73)
mode = os.environ.get("MODE", "")
head, remote_head, tag = "a" * 40, "b" * 40, "c" * 40
if name == "git":
    if args == ["rev-parse", "--show-toplevel"]: print(root if mode != "subdirectory" else root / "other")
    elif args == ["symbolic-ref", "--quiet", "--short", "HEAD"]:
        if mode == "detached": sys.exit(1)
        print("topic" if mode == "branch" else "main")
    elif args == ["rev-parse", "--verify", "HEAD"]:
        drift = mode == "head" or (mode == "fetch-head-drift" and (root / "fetched").exists()) or (mode == "tag-head-drift" and (root / "tagged").exists())
        print("d" * 40 if drift else head)
    elif args == ["status", "--porcelain", "--untracked-files=all"]:
        if mode in ("staged", "unstaged", "untracked"): print({"staged":"M  source.go", "unstaged":" M source.go", "untracked":"?? new.go"}[mode])
        if mode == "fetch-dirty" and (root / "fetched").exists(): print(" M source.go")
        if (root / "ci-dirty").exists(): print(" M source.go")
    elif args == ["show-ref", "--verify", "--quiet", "refs/tags/v0.66.0"]:
        sys.exit(0 if mode == "local-tag" else 1)
    elif args == ["remote", "get-url", "--push", "--all", "origin"]:
        print("https://github.com/example/router.git" if mode != "multiple-remotes" else "https://github.com/example/router.git\nmock://other")
    elif args == ["ls-remote", "--exit-code", "--refs", "https://github.com/example/router.git", "refs/tags/v0.66.0"]:
        sys.exit(0 if mode == "remote-tag" else 2)
    elif args == ["fetch", "--no-tags", "https://github.com/example/router.git", "refs/heads/main"]:
        (root / "fetched").touch()
        if mode == "fetch-version-drift": (root / ".version").write_text("0.67.0\n")
    elif args == ["rev-parse", "--verify", "FETCH_HEAD"]: print(remote_head)
    elif args == ["merge-base", "--is-ancestor", remote_head, head]:
        if mode == "diverged": sys.exit(1)
    elif args == ["tag", "-a", "v0.66.0", head, "-m", "Release v0.66.0"]: (root / "tagged").touch()
    elif args == ["rev-parse", "--verify", "refs/tags/v0.66.0"]: print(tag)
    elif args == ["rev-parse", "--verify", "refs/tags/v0.66.0^{commit}"]: print("d" * 40 if mode == "wrong-tag" else head)
    elif args[:4] == ["-c", "push.followTags=false", "push", "--atomic"]:
        want = ["-c", "push.followTags=false", "push", "--atomic", "--force-with-lease=refs/heads/main:"+remote_head, "--force-with-lease=refs/tags/v0.66.0:", "https://github.com/example/router.git", head+":refs/heads/main", tag+":refs/tags/v0.66.0"]
        if args != want: sys.exit(91)
    elif args[:3] == ["cliff", "--tag", "v0.66.0"] and args[3] == "--output":
        pathlib.Path(args[4]).write_text("" if mode == "empty-changelog" else "# [0.66.0]\nPrepared notes\n")
    else:
        print("Unexpected Git operation: " + repr(args), file=sys.stderr); sys.exit(92)
    sys.exit(0)
if name == "gh":
    from urllib.parse import parse_qs, urlsplit
    prefix = ["api", "--hostname", "github.com", "--method", "GET", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"]
    if args[:len(prefix)] != prefix: sys.exit(94)
    endpoint = args[len(prefix)]
    base = "repos/example/router/actions"
    run = dict(id=42, head_sha=head, head_branch="release-candidate/v0.66.0", event="push", path=".github/workflows/test.yml", repository={"full_name":"example/router"}, head_repository={"full_name":"example/router"}, status="completed", conclusion="success", run_attempt=2, html_url="https://github.com/example/router/actions/runs/42")
    changes = {"ci-sha":("head_sha", "d"*40), "ci-branch":("head_branch", "main"), "ci-pr":("event", "pull_request"), "ci-workflow":("path", ".github/workflows/other.yml"), "ci-pending":("status", "in_progress"), "ci-failed":("conclusion", "failure"), "ci-repo":("repository", {"full_name":"other/router"}), "ci-fork":("head_repository", {"full_name":"other/router"})}
    if mode in changes: run[changes[mode][0]] = changes[mode][1]
    if endpoint.startswith(base + "/workflows/test.yml/runs?"):
        if parse_qs(urlsplit(endpoint).query) != {"branch":["release-candidate/v0.66.0"], "head_sha":[head], "event":["push"], "per_page":["100"]}: sys.exit(95)
        runs = [run]
        if mode == "ci-none": runs = []
        if mode == "ci-old-green": runs = [dict(run, id=41), dict(run, status="queued", conclusion=None)]
        if mode == "ci-bad-json": print("not json"); sys.exit(0)
        print(json.dumps({"workflow_runs":runs}))
    elif endpoint == base + "/runs/42/jobs?filter=latest&per_page=100":
        if args[-2:] != ["--paginate", "--slurp"]: sys.exit(96)
        modules = json.loads((root / "tools/quality/nested-go-modules.json").read_text())
        names = ["release-workflow", "test", "sseclient", "nested-module-discovery"] + ["nested-modules ("+m+")" for m in modules]
        jobs = [dict(name=n, head_sha=head, run_id=42, status="completed", conclusion="success") for n in names]
        if mode == "ci-missing-job": jobs.pop()
        if mode == "ci-job-skipped": jobs[-1]["conclusion"] = "skipped"
        if mode == "ci-job-failed": jobs[-1]["conclusion"] = "failure"
        if mode == "ci-job-pending": jobs[-1]["status"] = "in_progress"
        if mode == "ci-job-sha": jobs[-1]["head_sha"] = "d"*40
        if mode == "ci-job-run": jobs[-1]["run_id"] = 41
        print(json.dumps([{"jobs":jobs[:3]}, {"jobs":jobs[3:]}]))
    elif endpoint == base + "/runs/42":
        if mode == "ci-rerun": run["run_attempt"] = 3
        if mode == "ci-rerun-pending": run["status"] = "queued"
        if mode == "ci-dirty": (root / "ci-dirty").touch()
        print(json.dumps(run))
    else: sys.exit(97)
    sys.exit(0)
if name == "npm":
    if args != ["version", "0.66.0", "--no-git-tag-version", "--allow-same-version", "--ignore-scripts"]: sys.exit(93)
    for filename in ["package.json", "package-lock.json"]:
        path = pathlib.Path(filename); data = json.loads(path.read_text()); data["version"] = args[1]
        if filename == "package-lock.json": data["packages"][""]["version"] = args[1]
        path.write_text(json.dumps(data))
    sys.exit(0)
# Only non-Git tools may execute, and only against the disposable fixture.
real = json.loads(os.environ["REAL_TOOLS"])[name]
sys.exit(subprocess.call([real, *args]))
'''


class ReleaseTests(unittest.TestCase):
    def run_release(self, action="release:strict", mode="", fail_at=0, args=None, extra="", version_bytes=None, unchanged=False):
        with tempfile.TemporaryDirectory(prefix="router-release-test-") as temp:
            path = Path(temp).resolve()
            version = "0.65.0" if action == "release:strict:prepare" else "0.66.0"
            (path / ".version").write_bytes(version_bytes if version_bytes is not None else (version + "\n").encode())
            for package in ["client", "sseclient"]:
                (path / package).mkdir()
                (path / package / "package.json").write_text(json.dumps({"version": version}))
                lock = {"version": version, "packages": {"": {"version": version}}}
                if mode == "lock-version": lock["version"] = "0.65.0"
                if mode == "lock-root": lock["packages"][""]["version"] = "0.65.0"
                (path / package / "package-lock.json").write_text(json.dumps(lock))
            (path / "websocket_client_handlers.go").write_text('package router\nconst (\n    WebSocketClientVersion = "' + version + '"\n)\n')
            (path / "CHANGELOG.md").write_text("# [" + ("0.65.0" if mode == "changelog" else version) + "]\n")
            if mode == "missing-version": (path / ".version").unlink()
            if mode == "same-version": (path / ".version").write_text("0.66.0\n")
            (path / "tools/quality").mkdir(parents=True)
            for filename in ["check_release_ci.py", "nested-go-modules.json"]:
                shutil.copyfile(ROOT / "tools/quality" / filename, path / "tools/quality" / filename)
            before = {str(p.relative_to(path)):p.read_bytes() for p in path.rglob("*") if p.is_file()}
            tools = ["node", "sed", "mv", "mktemp", "cat"]
            real = {tool: shutil.which(tool) for tool in tools}
            self.assertTrue(all(real.values()), "node and standard shell tools are required")
            bindir = path / "bin"; bindir.mkdir()
            for tool in ["git", "npm", "gh", *tools]:
                executable = bindir / tool; executable.write_text(MOCK); executable.chmod(0o755)
            env = dict(os.environ, PATH=str(bindir) + os.pathsep + os.environ["PATH"], FIXTURE=str(path), FAIL_AT=str(fail_at), MODE=mode, REAL_TOOLS=json.dumps(real), TMPDIR=str(path))
            # Test under conditional invocation too: Bash errexit cannot hide missing checks.
            script = 'set +e\nsource "$1"\nfunction lgr { :; }\n' + extra + '\nif "$2" "${@:3}"; then exit 0; else exit $?; fi\n'
            result = subprocess.run(["bash", "-c", script, "release-test", str(ROOT / "taskfile"), action, *(args if args is not None else ["0.66.0", HEAD])], cwd=path, env=env, text=True, capture_output=True, timeout=30)
            if unchanged:
                after = {str(p.relative_to(path)):p.read_bytes() for p in path.rglob("*") if p.is_file() and not p.is_relative_to(bindir) and p.name != "calls.jsonl"}
                self.assertEqual(after, before, "validation rewrote or created files")
            log = path / "calls.jsonl"
            calls = [json.loads(line) for line in log.read_text().splitlines()] if log.exists() else []
            files = {p: (path / p).read_text() for p in [".version", "CHANGELOG.md", "websocket_client_handlers.go"] if (path / p).exists()}
            for call in calls:
                if call[0] == "git":
                    self.assertNotIn(call[1], ["add", "commit", "checkout", "pull"], call)
                    self.assertNotIn("--tags", call, call)
            return result, calls, files

    @staticmethod
    def mutations(calls):
        return [c for c in calls if c[:2] == ["git", "tag"] or c[:4] == ["git", "-c", "push.followTags=false", "push"]]

    def test_publish_exact_commit_and_refs(self):
        result, calls, files = self.run_release()
        self.assertEqual(result.returncode, 0, result.stderr)
        mutations = self.mutations(calls)
        self.assertEqual(len(mutations), 2)
        self.assertEqual(mutations[0], ["git", "tag", "-a", "v0.66.0", HEAD, "-m", "Release v0.66.0"])
        self.assertEqual(mutations[1][-2:], [HEAD + ":refs/heads/main", TAG + ":refs/tags/v0.66.0"])
        self.assertIn("--atomic", mutations[1])
        self.assertIn("--force-with-lease=refs/heads/main:" + REMOTE_HEAD, mutations[1])
        self.assertIn("--force-with-lease=refs/tags/v0.66.0:", mutations[1])
        self.assertEqual(files[".version"], "0.66.0\n")
        self.assertFalse(any(c[0] == "npm" for c in calls))

    def test_publish_stops_at_each_external_failure(self):
        _, baseline, _ = self.run_release()
        for n, call in enumerate(baseline, 1):
            with self.subTest(boundary=n, call=call):
                result, calls, _ = self.run_release(fail_at=n)
                self.assertNotEqual(result.returncode, 0, (call, result.stdout))
                # Read-only helper pipelines may finish reading; no later Git mutation is allowed.
                self.assertFalse(self.mutations(calls[n:]), calls[n:])
                self.assertNotIn("Published", result.stdout)

    def test_publication_guards(self):
        for mode in ["subdirectory", "detached", "branch", "head", "staged", "unstaged", "untracked", "missing-version", "changelog", "lock-version", "lock-root", "local-tag", "multiple-remotes", "remote-tag", "diverged", "fetch-head-drift", "fetch-dirty", "fetch-version-drift", "wrong-tag", "tag-head-drift"]:
            with self.subTest(mode=mode):
                result, calls, _ = self.run_release(mode=mode)
                self.assertNotEqual(result.returncode, 0, (mode, result.stdout))
                self.assertFalse(any(c[:4] == ["git", "-c", "push.followTags=false", "push"] for c in calls))

    def test_explicit_arguments_required(self):
        for action in ["release:strict", "release:strict:prepare"]:
            for args in [[], ["minor"], ["0.66.0"], ["v0.66.0", HEAD], ["00.66.0", HEAD], ["0.66.0", "HEAD"], ["0.66.0", HEAD, "extra"]]:
                with self.subTest(action=action, args=args):
                    result, calls, _ = self.run_release(action=action, args=args)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(calls, [])

    def test_prepare_writes_reviewable_files_without_git_mutations(self):
        result, calls, files = self.run_release(action="release:strict:prepare")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(files[".version"], "0.66.0\n")
        self.assertIn('# [0.66.0]', files["CHANGELOG.md"])
        self.assertIn('WebSocketClientVersion = "0.66.0"', files["websocket_client_handlers.go"])
        self.assertEqual(self.mutations(calls), [])
        self.assertFalse(any(c[:2] in [["git", "fetch"], ["git", "ls-remote"]] for c in calls))

    def test_prepare_stops_at_each_external_failure(self):
        _, baseline, _ = self.run_release(action="release:strict:prepare")
        for n, call in enumerate(baseline, 1):
            with self.subTest(boundary=n, call=call):
                result, calls, _ = self.run_release(action="release:strict:prepare", fail_at=n)
                self.assertNotEqual(result.returncode, 0, (call, result.stdout))
                self.assertEqual(self.mutations(calls), [])
                self.assertNotIn("Prepared", result.stdout)
                # No later metadata/changelog mutation after any prerequisite fails.
                self.assertFalse(any(c[0] in ["npm", "mv"] or c[:2] in [["sed", "-i.bak"], ["git", "cliff"]] for c in calls[n:]), calls[n:])

    def test_prepare_guards_and_incomplete_changelog(self):
        for mode in ["head", "staged", "unstaged", "untracked", "branch", "local-tag", "same-version", "empty-changelog"]:
            with self.subTest(mode=mode):
                result, calls, files = self.run_release(action="release:strict:prepare", mode=mode)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(self.mutations(calls), [])
                self.assertNotIn("Prepared", result.stdout)
                self.assertEqual(files["CHANGELOG.md"], "# [0.65.0]\n")
                if mode not in ["same-version", "empty-changelog"]:
                    self.assertEqual(files[".version"], "0.65.0\n")

    def test_verification_never_initializes_or_repairs_metadata(self):
        invalid = [b"", b"\n", b"not-semver\n", b"00.66.0\n", b" 0.66.0\n", b"0.66.0\n0.66.0\n", b"0.66.0-beta.1\n", b"0.66.0\x00\n", b"0.66.0\n\n"]
        for action in ["version:verify-sync", "release:strict", "release:strict:prepare"]:
            for data in invalid:
                with self.subTest(action=action, data=data):
                    args = [] if action == "version:verify-sync" else None
                    result, calls, _ = self.run_release(action=action, args=args, version_bytes=data, unchanged=True)
                    self.assertNotEqual(result.returncode, 0)
                    self.assertEqual(self.mutations(calls), [])
                    self.assertFalse(any(c[0] in ["npm", "gh"] or c[:2] in [["git", "fetch"], ["git", "cliff"], ["git", "ls-remote"]] for c in calls))
            result, calls, files = self.run_release(action=action, mode="missing-version", args=[] if action == "version:verify-sync" else None, unchanged=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn(".version", files)
        for args in [[], ["0.66.0"], ["0.67.0"]]:
            result, _, _ = self.run_release(action="version:verify-sync", args=args, unchanged=True)
            self.assertEqual(result.returncode == 0, args != ["0.67.0"])

    def test_ci_must_qualify_final_commit_before_tagging(self):
        modes = ["ci-none", "ci-sha", "ci-branch", "ci-pr", "ci-workflow", "ci-pending", "ci-failed", "ci-repo", "ci-fork", "ci-old-green", "ci-bad-json", "ci-missing-job", "ci-job-skipped", "ci-job-failed", "ci-job-pending", "ci-job-sha", "ci-job-run", "ci-rerun", "ci-rerun-pending", "ci-dirty"]
        for mode in modes:
            with self.subTest(mode=mode):
                result, calls, _ = self.run_release(mode=mode)
                self.assertNotEqual(result.returncode, 0, result.stdout)
                self.assertEqual(self.mutations(calls), [])
                self.assertNotIn("Published", result.stdout)
        result, calls, _ = self.run_release()
        self.assertEqual(result.returncode, 0, result.stderr)
        checks = [i for i,c in enumerate(calls) if c[0] == "gh"]
        self.assertEqual(len(checks), 3)
        self.assertLess(max(checks), next(i for i,c in enumerate(calls) if c[:2] == ["git", "tag"]))

    def test_ci_repository_is_bound_to_push_endpoint(self):
        repository = runpy.run_path(str(ROOT / "tools/quality/check_release_ci.py"))["repository"]
        for remote in ["https://github.com/example/router.git", "https://github.com/example/router", "git@github.com:example/router.git", "ssh://git@github.com/example/router.git", "ssh://git@ssh.github.com:443/example/router.git"]:
            self.assertEqual(repository(remote), "example/router")
        for remote in ["https://github.com/other/example/router.git", "git@alias:example/router.git", "https://github.com.evil/example/router.git", "https://token@github.com/example/router.git", "file:///tmp/router", "-R example/router", "https://github.com/example/router.git\n"]:
            with self.assertRaises(ValueError): repository(remote)

    def test_explicit_version_initializer_is_preserved(self):
        for mode in ["", "missing-version"]:
            result, calls, files = self.run_release(action="version:get", mode=mode, args=[], version_bytes=b"")
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(files[".version"], "0.0.1\n")
            self.assertEqual(self.mutations(calls), [])

    def test_failed_version_write_cannot_continue(self):
        result, calls, _ = self.run_release(action="release:strict:prepare", extra='function version:set { return 73; }')
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(any(c[0] == "npm" or c[:2] == ["git", "cliff"] for c in calls))


if __name__ == "__main__":
    unittest.main(verbosity=2)

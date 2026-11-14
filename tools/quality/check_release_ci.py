#!/usr/bin/env python3
"""Read-only publication gate for exact-commit GitHub Actions evidence.

Uses authenticated `gh api` GET requests, bound to the actual push endpoint.
No source-commit, PR merge-commit or older successful attempt can qualify.
"""
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import urlencode


def repository(remote):
    # Deliberately support GitHub.com endpoints only; never query an unrelated
    # default gh repository or follow a local Git SSH alias to guessed evidence.
    match = re.fullmatch(
        r'(?:https://github\.com/|git@github\.com:|ssh://git@github\.com/|'
        r'ssh://git@ssh\.github\.com:443/)([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+?)(?:\.git)?', remote)
    if not match:
        raise ValueError('CI verification requires a canonical GitHub.com push URL')
    return match[1]


def api(endpoint, paginate=False):
    args = ['gh', 'api', '--hostname', 'github.com', '--method', 'GET',
            '-H', 'Accept: application/vnd.github+json',
            '-H', 'X-GitHub-Api-Version: 2022-11-28', endpoint]
    if paginate:
        args += ['--paginate', '--slurp']
    return json.loads(subprocess.check_output(args, text=True, timeout=60))


def verify(remote, version, sha):
    repo = repository(remote)
    branch = 'release-candidate/v' + version
    base = 'repos/' + repo + '/actions'
    query = urlencode(dict(branch=branch, head_sha=sha, event='push', per_page=100))
    runs = api(base + '/workflows/test.yml/runs?' + query)['workflow_runs']
    if not runs:
        raise ValueError('No Tests CI run for the final release commit on ' + branch)
    # Do not filter by success: a newer failed or pending run invalidates an old green run.
    run = max(runs, key=lambda item: item['id'])
    def valid(item):
        return (item['head_sha'] == sha and item['head_branch'] == branch
                and item['event'] == 'push' and item['path'] == '.github/workflows/test.yml'
                and item['repository']['full_name'].lower() == repo.lower()
                and item['head_repository']['full_name'].lower() == repo.lower()
                and item['status'] == 'completed' and item['conclusion'] == 'success')
    if not valid(run):
        raise ValueError('Latest Tests CI run does not qualify the exact release commit')
    run_path = base + '/runs/' + str(run['id'])
    pages = api(run_path + '/jobs?filter=latest&per_page=100', paginate=True)
    jobs = [job for page in pages for job in page['jobs']]
    modules = json.loads(Path('tools/quality/nested-go-modules.json').read_text())
    required = {'release-workflow', 'test', 'sseclient', 'nested-module-discovery'}
    required.update('nested-modules (' + module + ')' for module in modules)
    if (not required.issubset({job['name'] for job in jobs}) or
            any(job['head_sha'] != sha or job['run_id'] != run['id'] or
                job['status'] != 'completed' or job['conclusion'] != 'success' for job in jobs)):
        raise ValueError('Required CI jobs are missing, skipped, pending or unsuccessful')
    # A rerun started during the jobs query must not inherit the earlier verdict.
    current = api(run_path)
    if not valid(current) or current['run_attempt'] != run['run_attempt']:
        raise ValueError('CI changed during verification; check the latest attempt')
    print('Verified release CI: ' + run['html_url'] + ' at ' + sha)


if __name__ == '__main__':
    try:
        if len(sys.argv) != 4:
            raise ValueError('usage: check_release_ci.py PUSH_URL VERSION FULL_COMMIT')
        verify(*sys.argv[1:])
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print('Release CI verification failed: ' + str(error), file=sys.stderr)
        sys.exit(1)

#!/usr/bin/env python3
"""One-command local releases, with a persistent journal for interrupted work.

Preparation touches only an isolated tracked-source copy. Once applying metadata
starts, failures retain the journal and local state for `release:resume`; never
reset a user's branch/index or guess whether a failed push reached the server.
"""
import base64
import fcntl
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile

FILES = ('.version', 'CHANGELOG.md', 'client/package.json', 'client/package-lock.json',
         'sseclient/package.json', 'sseclient/package-lock.json', 'websocket_client_handlers.go')
VERSION = re.compile(r'(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)')


class ReleaseError(Exception):
    pass


def run(*args, cwd=None, allowed=(0,)):
    result = subprocess.run(args, cwd=cwd, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode not in allowed:
        raise ReleaseError('Command failed: ' + ' '.join(args[:3]) + '\n' + (result.stdout + result.stderr).decode(errors='replace'))
    return result


def git(*args, **kwargs):
    return run('git', *args, **kwargs).stdout.decode().strip()


def require(condition, message):
    if not condition:
        raise ReleaseError(message)


def tracked_clean():
    require(not git('status', '--porcelain', '--untracked-files=no'),
            'Commit or stash tracked changes before release. Untracked files are left alone.')


def version(text):
    value = text.removesuffix('\n')
    require(VERSION.fullmatch(value), 'Invalid stable version metadata')
    return value


def next_version(current, level):
    numbers = [int(n) for n in current.split('.')]
    index = {'major': 0, 'minor': 1, 'patch': 2}[level]
    numbers[index] += 1
    numbers[index + 1:] = [0] * (2 - index)
    return '.'.join(map(str, numbers))


def regular(path, root):
    require(path.is_file() and not path.is_symlink() and path.resolve().is_relative_to(root.resolve()),
            'Release metadata must be regular files inside the checkout: ' + str(path))


def snapshot(root):
    result = {}
    for name in FILES:
        path = root / name
        regular(path, root)
        result[name] = {'data': base64.b64encode(path.read_bytes()).decode(), 'mode': path.stat().st_mode & 0o777}
    return result


def atomic_write(path, data, mode=0o600):
    fd, temporary = tempfile.mkstemp(prefix='.release-', dir=path.parent)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, mode)
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def tag_exists(tag):
    return run('git', 'show-ref', '--verify', '--quiet', 'refs/tags/' + tag, allowed=(0, 1)).returncode == 0


def remote_refs(remote, branch, tag):
    lines = git('ls-remote', '--refs', remote, 'refs/heads/' + branch, 'refs/tags/' + tag)
    return dict((ref, oid) for oid, ref in (line.split() for line in lines.splitlines()))


class Release:
    def __init__(self, root, directory):
        self.root, self.directory = root, directory
        self.path = directory / 'state.json'
        self.state = None

    def save(self):
        atomic_write(self.path, (json.dumps(self.state, indent=2) + '\n').encode())

    def prepare(self, level, message):
        require(not self.path.exists(), 'An unfinished release exists. Run ./taskfile release:resume; do not bump again.')
        tracked_clean()
        branch = git('symbolic-ref', '--quiet', '--short', 'HEAD')
        remote = git('remote', 'get-url', '--push', '--all', 'origin')
        require(remote and '\n' not in remote and not remote.startswith('-'), 'Release needs one origin push URL')
        # Same endpoint for synchronization, tag checks and publication. Never force-fetch tags.
        git('fetch', '--tags', remote, 'refs/heads/' + branch)
        remote_head = git('rev-parse', '--verify', 'FETCH_HEAD')
        git('merge', '--ff-only', remote_head)
        tracked_clean()
        require(git('symbolic-ref', '--quiet', '--short', 'HEAD') == branch, 'Branch changed during synchronization')
        source = git('rev-parse', '--verify', 'HEAD')
        git('ls-files', '--error-unmatch', '--', *FILES, 'taskfile', 'tools/quality/manual_release.py')
        original = snapshot(self.root)
        current = version((self.root / '.version').read_text())
        # Include tags in the base, like go-admin, so stale metadata cannot recycle a version.
        versions = [current]
        for tag in git('tag', '--list', 'v*').splitlines():
            if VERSION.fullmatch(tag[1:]):
                versions.append(tag[1:])
        base = max(versions, key=lambda value: tuple(map(int, value.split('.'))))
        target = next_version(base, level)
        tag = 'v' + target
        require(not tag_exists(tag), 'Release tag already exists locally: ' + tag)
        refs = remote_refs(remote, branch, tag)
        require('refs/tags/' + tag not in refs, 'Release tag already exists remotely: ' + tag)
        require(refs.get('refs/heads/' + branch) == remote_head, 'Remote branch changed; retry after synchronization')
        # No live metadata writes or commits during preparation. TMPDIR may point
        # into the checkout, so explicitly use external temporary storage.
        with tempfile.TemporaryDirectory(prefix='go-router-release-', dir='/tmp') as temp:
            prepared = Path(temp) / 'source'
            prepared.mkdir()
            git('checkout-index', '--all', '--prefix=' + str(prepared) + '/')
            for name in FILES:
                regular(prepared / name, prepared)
            script = ('source "$1"\nVERSION_FILE=.version\n'
                      'function lgr { printf "%s\\n" "$*"; }\n'
                      'version:verify-sync && version:sync "$2"')
            run('bash', '-c', script, 'release-metadata', str(prepared / 'taskfile'), target, cwd=prepared)
            git('cliff', '--tag', tag, '--output', str(prepared / 'CHANGELOG.md'))
            require('# [' + target + ']' in (prepared / 'CHANGELOG.md').read_text(), 'Generated changelog is missing the release entry')
            outputs = snapshot(prepared)
        tracked_clean()
        require(git('rev-parse', '--verify', 'HEAD') == source, 'HEAD changed during preparation')
        require(snapshot(self.root) == original, 'Metadata changed during preparation')
        self.directory.mkdir(exist_ok=True)
        self.state = dict(root=str(self.root), branch=branch, remote=remote, remote_head=remote_head,
                          source=source, version=target, tag=tag, original=original, prepared=outputs,
                          message=message or 'New ' + level + ' release: ' + tag)
        self.save()  # Journal is present before the first live write.

    def load(self):
        require(self.path.is_file(), 'No resumable release journal found')
        self.state = json.loads(self.path.read_text())
        require(self.state['root'] == str(self.root), 'Release journal belongs to another checkout')

    def assert_branch(self):
        s = self.state
        require(git('symbolic-ref', '--quiet', '--short', 'HEAD') == s['branch'], 'Return to the original release branch before resuming')
        require(git('remote', 'get-url', '--push', '--all', 'origin') == s['remote'], 'Origin push endpoint changed; inspect the release journal')

    def commit(self):
        s = self.state
        self.assert_branch()
        head = git('rev-parse', '--verify', 'HEAD')
        if head == s['source']:
            # A failed git add/commit may have left some of the owned metadata
            # staged. Accept only original/prepared bytes; never overwrite edits.
            for flag in ([], ['--cached']):
                changed = git('diff', *flag, '--name-only', '-z').split('\0')
                require(set(filter(None, changed)).issubset(FILES), 'Unrelated tracked edits appeared; preserve them and reconcile before resuming')
            live = snapshot(self.root)
            for name in FILES:
                require(live[name] in (s['original'][name], s['prepared'][name]), 'Metadata edited outside the release: ' + name)
                staged = base64.b64encode(run('git', 'show', ':' + name).stdout).decode()
                require(staged in (s['original'][name]['data'], s['prepared'][name]['data']), 'Unexpected staged metadata: ' + name)
            for name in FILES:
                content = s['prepared'][name]
                atomic_write(self.root / name, base64.b64decode(content['data']), content['mode'])
            git('add', '--', *FILES)
            require(set(filter(None, git('diff', '--cached', '--name-only', '-z').split('\0'))).issubset(FILES),
                    'Unexpected staged source before commit; preserve and reconcile the index')
            for name in FILES:
                require(run('git', 'show', ':' + name).stdout == base64.b64decode(s['prepared'][name]['data']),
                        'Staged metadata changed before commit: ' + name)
            require(git('rev-parse', '--verify', 'HEAD') == s['source'], 'HEAD changed before release commit')
            s['tree'] = git('write-tree')
            self.save()
            git('commit', '-m', 'Bump version: ' + s['tag'])
            head = git('rev-parse', '--verify', 'HEAD')
        # Also recognizes a successful commit whose acknowledgement was lost.
        require(s.get('tree') and git('rev-parse', '--verify', head + '^{tree}') == s['tree']
                and git('rev-parse', '--verify', head + '^') == s['source'],
                'HEAD is not the prepared release commit. Journal retained; inspect hook/concurrent changes.')
        tracked_clean()
        self.assert_branch()
        s['commit'] = head
        self.save()

    def publish(self):
        s = self.state
        self.assert_branch()
        tracked_clean()
        require(git('rev-parse', '--verify', 'HEAD') == s['commit'], 'Release HEAD changed')
        if not tag_exists(s['tag']):
            git('tag', '-a', s['tag'], s['commit'], '-m', s['message'])
        ref = 'refs/tags/' + s['tag']
        require(git('cat-file', '-t', ref) == 'tag' and git('rev-parse', '--verify', ref + '^{commit}') == s['commit'],
                'Local release tag conflicts with the prepared commit; never retarget it automatically')
        obj = git('rev-parse', '--verify', ref)
        require(not s.get('tag_object') or obj == s['tag_object'], 'Local release tag was replaced')
        s['tag_object'] = obj
        self.save()
        refs = remote_refs(s['remote'], s['branch'], s['tag'])
        branch_ref = 'refs/heads/' + s['branch']
        if ref in refs:
            require(refs[ref] == obj, 'Remote release tag conflicts; no overwrite is allowed')
            # A dropped push acknowledgement can leave publication complete. The
            # branch may have advanced since; require it still contains our commit.
            git('fetch', '--no-tags', s['remote'], branch_ref)
            remote_head = git('rev-parse', '--verify', 'FETCH_HEAD')
            git('merge-base', '--is-ancestor', s['commit'], remote_head)
            print('Release already published: ' + s['tag'])
        else:
            require(refs.get(branch_ref) == s['remote_head'], 'Remote branch changed; publication stopped. Keep the journal and reconcile remote history.')
            self.assert_branch()
            tracked_clean()
            require(git('rev-parse', '--verify', 'HEAD') == s['commit'], 'Release HEAD changed before push')
            git('-c', 'push.followTags=false', 'push', '--atomic',
                '--force-with-lease=' + branch_ref + ':' + s['remote_head'],
                '--force-with-lease=' + ref + ':', s['remote'],
                s['commit'] + ':' + branch_ref, obj + ':' + ref)
            print('Published ' + s['tag'] + ' at ' + s['commit'])
        # Only our own journal is removed. A cleanup failure remains resumable.
        self.path.unlink()


def main(args):
    require(args and args[0] in ('start', 'resume'), 'Expected start or resume')
    mode, values = args[0], args[1:]
    level = values[0] if values else 'patch'
    require((mode == 'resume' and not values) or (mode == 'start' and len(values) <= 2 and level in ('major', 'minor', 'patch')),
            'usage: ./taskfile release [major|minor|patch] [message], or ./taskfile release:resume')
    root = Path(git('rev-parse', '--show-toplevel')).resolve()
    require(root == Path.cwd().resolve(), 'Run release from the repository root')
    directory = Path(git('rev-parse', '--absolute-git-dir'))
    with (directory / 'go-router-release.lock').open('a') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError as error:
            raise ReleaseError('Another manual release is running') from error
        release = Release(root, directory / 'go-router-release')
        try:
            if mode == 'start':
                release.prepare(level, values[1] if len(values) == 2 else '')
            else:
                release.load()
            release.commit()
            release.publish()
        except BaseException:
            if release.path.exists():
                print('Release stopped. Local commit/tag/metadata and recovery journal are preserved.\n'
                      'Resolve the reported prerequisite, then run ./taskfile release:resume (no new bump).\n'
                      'Do not reset/delete release refs after an uncertain push. Journal: ' + str(release.path), file=sys.stderr)
            raise


if __name__ == '__main__':
    try:
        main(sys.argv[1:])
    except (ReleaseError, OSError, ValueError, KeyError, KeyboardInterrupt) as error:
        print('Release failed: ' + str(error), file=sys.stderr)
        sys.exit(1)

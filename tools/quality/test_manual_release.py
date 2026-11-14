#!/usr/bin/env python3
"""Stateful release/recovery regressions. Git is entirely inert, including commits.

The real manual orchestrator and filesystem journal run against disposable files.
A metadata smoke runs the real taskfile helpers with Node and inert npm; no Git
process, push, network call or application-repository mutation is performed.
"""
import contextlib
import hashlib
import io
import json
import os
from pathlib import Path
import runpy
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
NS = runpy.run_path(str(ROOT / 'tools/quality/manual_release.py'))
MAIN = NS['main']
GLOBALS = MAIN.__globals__
ERROR = NS['ReleaseError']
FILES = NS['FILES']
SOURCE, COMMIT, TAG = 'a'*40, 'c'*40, 'd'*40


class Fixture:
    def __init__(self, root, real_metadata=False, real_npm=False):
        self.root = root
        self.real_metadata = real_metadata
        self.real_npm = real_npm
        (root / '.git').mkdir()
        for name in FILES:
            path = root / name
            path.parent.mkdir(exist_ok=True)
            if name == '.version': data = '0.65.0\n'
            elif name == 'CHANGELOG.md': data = '# [0.65.0]\n'
            elif name.endswith('.go'): data = 'package router\nconst (\n WebSocketClientVersion = "0.65.0"\n)\n'
            elif name.endswith('package-lock.json'): data = json.dumps({'version':'0.65.0','packages':{'':{'version':'0.65.0'}}})
            else: data = json.dumps({'version':'0.65.0'})
            path.write_text(data)
        shutil.copyfile(ROOT / 'taskfile', root / 'taskfile')
        (root/'tools/quality').mkdir(parents=True)
        shutil.copyfile(ROOT/'tools/quality/manual_release.py',root/'tools/quality/manual_release.py')
        (root / 'source.go').write_text('package router\n')
        (root / 'untracked-notes.txt').write_text('keep me\n')
        self.names = [*FILES, 'taskfile', 'source.go', 'tools/quality/manual_release.py']
        self.index = self.read()
        self.original = dict(self.index)
        self.committed = dict(self.index)
        self.head, self.remote_head, self.tag, self.remote_tag = SOURCE, SOURCE, None, None
        self.tag_commit = COMMIT
        self.branch = 'main'
        self.remote = 'mock://origin'
        self.calls, self.fail_at, self.mode = [], 0, ''
        self.commit_count = self.tag_count = self.push_count = 0

    def read(self):
        return {name:(self.root/name).read_bytes() for name in self.names}

    @staticmethod
    def tree(files):
        return hashlib.sha256(b''.join(n.encode()+v for n,v in sorted(files.items()))).hexdigest()

    def call(self, *args, cwd=None, allowed=(0,)):
        self.calls.append(list(args))
        if self.fail_at == len(self.calls):
            raise ERROR('injected command failure')
        output, code = '', 0
        if args[0] == 'bash':
            target = args[-1]
            if self.real_metadata:
                bindir = self.root / 'fake-bin'; bindir.mkdir(exist_ok=True)
                npm = bindir / 'npm'
                npm.write_text('''#!/usr/bin/env python3
import json, pathlib, sys
assert sys.argv[1] == 'version' and '--ignore-scripts' in sys.argv
for name in ['package.json','package-lock.json']:
 p=pathlib.Path(name);d=json.loads(p.read_text());d['version']=sys.argv[2]
 if name=='package-lock.json': d['packages']['']['version']=sys.argv[2]
 p.write_text(json.dumps(d))
''')
                npm.chmod(0o755)
                if self.real_npm:
                    npm.unlink()
                    blocker=bindir/'git';blocker.write_text('#!/bin/sh\nexit 94\n');blocker.chmod(0o755)
                for config in ['npm-userconfig','npm-globalconfig']: (self.root/config).touch()
                env = dict(os.environ, PATH=str(bindir)+os.pathsep+os.environ['PATH'], NPM_CONFIG_OFFLINE='true', NPM_CONFIG_CACHE=str(self.root/'npm-cache'), NPM_CONFIG_USERCONFIG=str(self.root/'npm-userconfig'), NPM_CONFIG_GLOBALCONFIG=str(self.root/'npm-globalconfig'))
                result = subprocess.run(args, cwd=cwd, env=env, capture_output=True)
                if result.returncode: raise ERROR(result.stderr.decode())
            else:
                for name in FILES:
                    if name != 'CHANGELOG.md':
                        path = Path(cwd)/name
                        path.write_text(path.read_text().replace('0.65.0',target))
        elif args[0] != 'git':
            raise AssertionError('Unexpected external tool: '+repr(args))
        else:
            a=list(args[1:])
            if a==['rev-parse','--show-toplevel']: output=str(self.root)
            elif a==['rev-parse','--absolute-git-dir']: output=str(self.root/'.git')
            elif a==['status','--porcelain','--untracked-files=no']:
                output=' M tracked' if self.read()!=self.index or self.index!=self.committed else ''
            elif a==['symbolic-ref','--quiet','--short','HEAD']: output=self.branch
            elif a==['remote','get-url','--push','--all','origin']: output=self.remote
            elif a==['fetch','--tags',self.remote,'refs/heads/'+self.branch]: pass
            elif a==['fetch','--no-tags',self.remote,'refs/heads/'+self.branch]: pass
            elif a==['rev-parse','--verify','FETCH_HEAD']: output=self.remote_head
            elif a==['merge','--ff-only',SOURCE]: pass
            elif a==['rev-parse','--verify','HEAD']: output=self.head
            elif a==['ls-files','--error-unmatch','--',*FILES,'taskfile','tools/quality/manual_release.py']: output='\n'.join(FILES)
            elif a==['tag','--list','v*']: output='v0.65.0\n'+('v0.70.0' if self.mode=='newer-tag' else '')
            elif a[:3]==['show-ref','--verify','--quiet']:
                code=0 if self.tag else 1
            elif a[:2]==['ls-remote','--refs']:
                assert a[2]==self.remote
                output=self.remote_head+'\trefs/heads/'+self.branch
                if self.remote_tag: output+='\n'+self.remote_tag+'\t'+a[-1]
                if self.mode=='remote-drift' and self.tag: output='e'*40+'\trefs/heads/'+self.branch
            elif a[:2]==['checkout-index','--all']:
                destination=Path(a[2].removeprefix('--prefix='))
                for name,data in self.index.items():
                    path=destination/name;path.parent.mkdir(parents=True,exist_ok=True);path.write_bytes(data)
            elif a[:2]==['cliff','--tag']:
                Path(a[-1]).write_text('# ['+a[2][1:]+']\nPrepared notes\n')
                if self.mode=='source-drift': (self.root/'source.go').write_text('concurrent edit\n')
            elif a==['diff','--name-only','-z']:
                output='\0'.join(name for name in self.names if self.read()[name]!=self.index[name])
            elif a==['diff','--cached','--name-only','-z']:
                output='\0'.join(name for name in self.names if self.index[name]!=self.committed[name])
            elif a[0]=='show' and a[1].startswith(':'): output=self.index[a[1][1:]]
            elif a==['add','--',*FILES]:
                self.index.update({n:(self.root/n).read_bytes() for n in FILES})
                if self.mode=='index-drift': self.index['source.go']=b'concurrent staged edit\n'
            elif a==['write-tree']: output=self.tree(self.index)
            elif a[:2]==['commit','-m']:
                if self.mode=='hook-stage': self.index['source.go']=b'hook staged edit\n'
                self.head=COMMIT;self.committed=dict(self.index);self.commit_count+=1
                if self.mode=='commit-ack': self.mode='';raise ERROR('lost commit acknowledgement')
                if self.mode=='hook-edit': (self.root/'source.go').write_text('hook edit\n')
            elif a==['rev-parse','--verify',COMMIT+'^{tree}']: output=self.tree(self.committed)
            elif a==['rev-parse','--verify',COMMIT+'^']: output=SOURCE
            elif a[:2]==['tag','-a']:
                assert a[3]==COMMIT
                self.tag=TAG;self.tag_count+=1
                if self.mode=='tag-ack': self.mode='';raise ERROR('lost tag acknowledgement')
            elif a[:2]==['cat-file','-t']: output='tag'
            elif a[:2]==['rev-parse','--verify'] and a[2].endswith('^{commit}'): output=self.tag_commit
            elif a[:2]==['rev-parse','--verify'] and a[2].startswith('refs/tags/'): output=self.tag
            elif a[:4]==['-c','push.followTags=false','push','--atomic']:
                assert a[4]=='--force-with-lease=refs/heads/'+self.branch+':'+SOURCE
                assert a[5].startswith('--force-with-lease=refs/tags/v') and a[5].endswith(':')
                assert a[6]==self.remote and a[7]==COMMIT+':refs/heads/'+self.branch
                assert a[8].startswith(TAG+':refs/tags/v')
                self.remote_head=COMMIT;self.remote_tag=TAG;self.push_count+=1
                if self.mode=='push-ack': self.mode='';raise ERROR('lost push acknowledgement')
            elif a==['merge-base','--is-ancestor',COMMIT,COMMIT]: pass
            else: raise AssertionError('Unexpected Git call: '+repr(a))
        if code not in allowed: raise ERROR('Git lookup failure')
        if isinstance(output,str): output=output.encode()
        return subprocess.CompletedProcess(args,code,output,b'')

    @contextlib.contextmanager
    def active(self):
        previous=Path.cwd();os.chdir(self.root)
        try:
            with patch.dict(GLOBALS,run=self.call), contextlib.redirect_stdout(io.StringIO()), contextlib.redirect_stderr(io.StringIO()):
                yield
        finally: os.chdir(previous)

    @property
    def journal(self): return self.root/'.git/go-router-release/state.json'


class ManualReleaseTests(unittest.TestCase):
    @contextlib.contextmanager
    def fixture(self, **kwargs):
        with tempfile.TemporaryDirectory(prefix='router-manual-test-') as temp:
            fixture=Fixture(Path(temp).resolve(),**kwargs)
            with fixture.active(): yield fixture

    def test_old_command_levels_default_and_custom_message(self):
        for args,target in [([], '0.65.1'),(['patch'],'0.65.1'),(['minor'],'0.66.0'),(['major','Custom notes'],'1.0.0')]:
            with self.subTest(args=args), self.fixture() as f:
                MAIN(['start',*args])
                self.assertEqual((f.root/'.version').read_text(),target+'\n')
                self.assertIn('# ['+target+']',f.committed['CHANGELOG.md'].decode())
                self.assertEqual((f.commit_count,f.tag_count,f.push_count),(1,1,1))
                self.assertFalse(f.journal.exists())
                self.assertEqual((f.root/'untracked-notes.txt').read_text(),'keep me\n')
                if len(args)==2: self.assertIn(['git','tag','-a','v'+target,COMMIT,'-m','Custom notes'],f.calls)

    def test_every_command_failure_is_recoverable_without_another_bump(self):
        with self.fixture() as baseline:
            MAIN(['start','minor']); calls=baseline.calls
        for n,call in enumerate(calls,1):
            with self.subTest(boundary=n,call=call), self.fixture() as f:
                f.fail_at=n
                with self.assertRaises(ERROR): MAIN(['start','minor'])
                self.assertEqual(f.push_count,0)
                f.fail_at=0
                if f.journal.exists():
                    with self.assertRaises(ERROR): MAIN(['start','minor'])
                    MAIN(['resume'])
                else:
                    self.assertEqual(f.read(),f.original)
                    MAIN(['start','minor'])
                self.assertEqual((f.root/'.version').read_text(),'0.66.0\n')
                self.assertEqual((f.commit_count,f.tag_count,f.push_count),(1,1,1))

    def test_lost_commit_tag_and_push_acknowledgements(self):
        for mode in ['commit-ack','tag-ack','push-ack']:
            with self.subTest(mode=mode), self.fixture() as f:
                f.mode=mode
                with self.assertRaises(ERROR): MAIN(['start','minor'])
                self.assertTrue(f.journal.exists())
                MAIN(['resume'])
                self.assertEqual((f.commit_count,f.tag_count,f.push_count),(1,1,1))
                self.assertFalse(f.journal.exists())

    def test_partial_file_application_is_resumable(self):
        with self.fixture() as f:
            actual=GLOBALS['atomic_write']
            def broken(path,*args,**kwargs):
                if path==f.root/'client/package-lock.json': raise OSError('disk full')
                return actual(path,*args,**kwargs)
            with patch.dict(GLOBALS,atomic_write=broken), self.assertRaises(OSError): MAIN(['start','minor'])
            self.assertTrue(f.journal.exists())
            MAIN(['resume'])
            self.assertEqual((f.commit_count,f.tag_count,f.push_count),(1,1,1))

    def test_tracked_edits_are_rejected_before_fetch(self):
        for staged in [False,True]:
            with self.fixture() as f:
                (f.root/'source.go').write_text('preserve edit\n')
                if staged: f.index['source.go']=b'preserve edit\n'
                with self.assertRaises(ERROR): MAIN(['start','patch'])
                self.assertFalse(any(c[:2]==['git','fetch'] for c in f.calls))
                self.assertEqual((f.root/'source.go').read_text(),'preserve edit\n')

    def test_preparation_and_hook_drift_are_preserved(self):
        for mode in ['source-drift','hook-edit','index-drift','hook-stage']:
            with self.fixture() as f:
                f.mode=mode
                with self.assertRaises(ERROR): MAIN(['start','minor'])
                self.assertEqual(f.push_count,0)
                self.assertNotEqual(f.index['source.go'] if mode in ['index-drift','hook-stage'] else (f.root/'source.go').read_bytes(),f.original['source.go'])

    def test_remote_drift_stops_publication_and_can_resume(self):
        with self.fixture() as f:
            f.mode='remote-drift'
            with self.assertRaises(ERROR): MAIN(['start','minor'])
            self.assertEqual(f.push_count,0)
            self.assertTrue(f.journal.exists())
            f.mode='';MAIN(['resume'])
            self.assertEqual((f.commit_count,f.tag_count,f.push_count),(1,1,1))

    def test_resume_does_not_overwrite_new_metadata_edits(self):
        with self.fixture() as f:
            f.mode='commit-ack'
            with self.assertRaises(ERROR): MAIN(['start','minor'])
            (f.root/'client/package.json').write_text('preserve edit\n')
            with self.assertRaises(ERROR): MAIN(['resume'])
            self.assertEqual((f.root/'client/package.json').read_text(),'preserve edit\n')
            self.assertEqual(f.push_count,0)

    def test_newer_existing_version_tag_sets_bump_base(self):
        with self.fixture() as f:
            f.mode='newer-tag';MAIN(['start','patch'])
            self.assertEqual((f.root/'.version').read_text(),'0.70.1\n')

    def test_real_metadata_helpers_in_isolated_source(self):
        with self.fixture(real_metadata=True) as f:
            MAIN(['start','minor'])
            for name in ['client/package-lock.json','sseclient/package-lock.json']:
                self.assertEqual(json.loads((f.root/name).read_text())['packages']['']['version'],'0.66.0')
            self.assertIn(b'"0.66.0"',f.committed['websocket_client_handlers.go'])

    def test_real_offline_npm_metadata_smoke(self):
        with self.fixture(real_metadata=True,real_npm=True) as f:
            MAIN(['start','minor'])
            self.assertEqual(json.loads((f.root/'client/package-lock.json').read_text())['version'],'0.66.0')
            self.assertEqual(f.commit_count,1)

    def test_current_release_branch_is_retained(self):
        with self.fixture() as f:
            f.branch='release/maintenance';MAIN(['start','patch'])
            self.assertTrue(any(c[-2]==COMMIT+':refs/heads/release/maintenance' for c in f.calls if c[:4]==['git','-c','push.followTags=false','push']))

    def test_conflicting_local_tag_is_never_retargeted(self):
        with self.fixture() as f:
            f.mode='commit-ack'
            with self.assertRaises(ERROR): MAIN(['start','minor'])
            f.tag=TAG;f.tag_commit='e'*40
            with self.assertRaises(ERROR): MAIN(['resume'])
            self.assertEqual(f.tag_commit,'e'*40)
            self.assertEqual((f.tag_count,f.push_count),(0,0))

    def test_remote_tag_conflict_is_never_overwritten(self):
        with self.fixture() as f:
            f.mode='commit-ack'
            with self.assertRaises(ERROR): MAIN(['start','minor'])
            f.remote_tag='e'*40
            with self.assertRaises(ERROR): MAIN(['resume'])
            self.assertEqual(f.remote_tag,'e'*40)
            self.assertEqual(f.push_count,0)
            self.assertTrue(f.journal.exists())

    def test_concurrent_manual_release_is_rejected(self):
        with self.fixture() as f:
            with (f.root/'.git/go-router-release.lock').open('a') as lock:
                NS['fcntl'].flock(lock,NS['fcntl'].LOCK_EX|NS['fcntl'].LOCK_NB)
                with self.assertRaises(ERROR): MAIN(['start','minor'])
                self.assertFalse(any(c[:2]==['git','fetch'] for c in f.calls))

    def test_taskfile_preserves_manual_dispatch_and_argument_quoting(self):
        with tempfile.TemporaryDirectory(prefix='router-release-dispatch-') as temp:
            directory=Path(temp)
            launcher=directory/'python3'
            launcher.write_text('#!'+sys.executable+'\nimport json,sys\nprint(json.dumps(sys.argv[1:]))\n')
            launcher.chmod(0o755)
            for args,want in [(['release'],['start']),(['release','minor','custom words'],['start','minor','custom words']),(['release:resume'],['resume'])]:
                result=subprocess.run(['bash','-c','source "$1"; shift; "$@"','dispatch',str(ROOT/'taskfile'),*args],cwd=directory,env=dict(os.environ,PATH=str(directory)+os.pathsep+os.environ['PATH']),capture_output=True,text=True)
                self.assertEqual(result.returncode,0,result.stderr)
                called=json.loads(result.stdout)
                self.assertEqual(called,[str(ROOT/'tools/quality/manual_release.py'),*want])

    def test_arguments_cannot_start_git_mutations(self):
        for args in [['start','0.66.0'],['start','minor','msg','extra'],['resume','patch']]:
            with self.fixture() as f:
                with self.assertRaises(ERROR): MAIN(args)
                self.assertEqual(f.calls,[])


if __name__=='__main__': unittest.main(verbosity=2)

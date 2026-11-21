"""Exercise the actual installer in a temporary filesystem with fake systemd.

Only the three fixed system paths are relocated. Linux command shims let this
run on macOS too; real systemd/service sandboxing still needs a Linux smoke test.
"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

REPO = Path(__file__).resolve().parents[2]

SHIM = r'''import fcntl, hashlib, json, os, pathlib, sys
p = pathlib.Path
cmd, args = p(sys.argv[0]).name, sys.argv[1:]
root = p(os.environ['INSTALL_TEST_ROOT'])
statefile = root / 'state.json'
s = json.loads(statefile.read_text())
if cmd == 'id': print('0')
elif cmd == 'uname': print('Linux')
elif cmd == 'sha256sum': print(hashlib.sha256(p(args[-1]).read_bytes()).hexdigest() + '  ' + args[-1])
elif cmd == 'readlink': print(p(args[-1]).resolve())
elif cmd == 'mv': os.replace(args[-2], args[-1])
elif cmd == 'sleep': pass
elif cmd == 'flock':
    try: fcntl.flock(int(args[-1]), fcntl.LOCK_EX | fcntl.LOCK_NB)
    except BlockingIOError: sys.exit(1)
elif cmd == 'curl': print('{"ok":true,"service":"homing-server"}')
elif cmd == 'install':
    if s.get('interrupt_copy') and args[-2] == str(root / 'binary'):
        p(args[-1]).write_bytes(b'truncated')
        sys.exit(99)
    os.execv(os.environ['INSTALL_TEST_INSTALL'], ['install'] + args)
elif cmd == 'systemctl':
    s.setdefault('calls', []).append(args)
    command = args[0]
    code = 0
    if command == 'show':
        prop = next(x.split('=', 1)[1] for x in args if x.startswith('--property='))
        defaults = {'FragmentPath': str(root / 'system/homing-relay.service') if (root / 'system/homing-relay.service').exists() else '', 'DropInPaths':'', 'LoadState':'loaded', 'UnitFileState':'enabled' if s.get('enabled') else 'disabled', 'Names':'homing-relay.service'}
        print(s.get(prop, defaults[prop]))
    elif command == 'is-active': code = 0 if s.get('active') else 3
    elif command == 'is-enabled':
        print('enabled' if s.get('enabled') else 'disabled')
        code = 0 if s.get('enabled') else 1
    elif command == 'restart':
        target = (root / 'base/current').resolve().name
        s['active'] = target != s.get('fail_revision')
        code = 0 if s['active'] else 1
    elif command == 'stop': s['active'] = False
    elif command == 'enable': s['enabled'] = True
    elif command == 'disable': s['enabled'] = False
    statefile.write_text(json.dumps(s))
    sys.exit(code)
else: raise RuntimeError(cmd)
'''

class InstallerTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='homing-installer-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        for name in ('scripts', 'deploy', 'bin', 'system', 'locks'):
            (self.root / name).mkdir()
        source = (REPO / 'scripts/install-relay.sh').read_text()
        for old, new in (
            ('base=/opt/homing-relay', f'base={self.root}/base'),
            ('unit=/etc/systemd/system/homing-relay.service', f'unit={self.root}/system/homing-relay.service'),
            ('lock=/run/lock/homing-relay-install.lock', f'lock={self.root}/locks/install.lock'),
        ):
            source = source.replace(old, new)
        self.script = self.root / 'scripts/install-relay.sh'
        self.script.write_text(source)
        shutil.copy(REPO / 'deploy/homing-relay.service', self.root / 'deploy')
        self.binary = self.root / 'binary'
        self.binary.write_bytes(b'demo-relay-binary')
        self.sha = hashlib.sha256(self.binary.read_bytes()).hexdigest()
        self.state = self.root / 'state.json'
        self.save({})
        self.env = dict(os.environ, INSTALL_TEST_ROOT=str(self.root), INSTALL_TEST_INSTALL=shutil.which('install'), PATH=str(self.root / 'bin') + os.pathsep + os.environ['PATH'])
        for cmd in ('id', 'uname', 'sha256sum', 'readlink', 'mv', 'sleep', 'flock', 'curl', 'install', 'systemctl'):
            path = self.root / 'bin' / cmd
            path.write_text('#!' + os.sys.executable + '\n' + SHIM)
            path.chmod(0o755)

    def save(self, state): self.state.write_text(json.dumps(state))
    def load(self): return json.loads(self.state.read_text())
    def run_install(self, revision='demo-new'):
        return subprocess.run(['bash', str(self.script), str(self.binary), self.sha, revision], env=self.env, capture_output=True, text=True, timeout=15)
    def seed(self, active):
        release = self.root / 'base/releases/demo-old'
        release.mkdir(parents=True)
        old = release / 'homing-server'
        old.write_bytes(b'demo-old-binary')
        old.chmod(0o755)
        (self.root / 'base/current').symlink_to(release)
        shutil.copy(self.root / 'deploy/homing-relay.service', self.root / 'system')
        self.save({'active': active, 'enabled': True, 'fail_revision': 'demo-new'})

    def test_refuses_foreign_units_before_writing(self):
        for state in (
            {'FragmentPath':'/usr/lib/systemd/system/homing-relay.service'},
            {'FragmentPath':'/run/systemd/system/homing-relay.service'},
            {'DropInPaths':'/etc/systemd/system/homing-relay.service.d/override.conf'},
            {'LoadState':'masked'},
            {'Names':'homing-relay.service unrelated.service'},
        ):
            with self.subTest(state=state):
                self.save(state)
                result = self.run_install()
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertFalse((self.root / 'base/current').exists())
                self.assertFalse((self.root / 'system/homing-relay.service').exists())

    def test_first_failure_restores_absence_then_retry_succeeds(self):
        self.save({'fail_revision':'demo-new'})
        self.assertNotEqual(self.run_install().returncode, 0)
        self.assertFalse((self.root / 'base/current').is_symlink())
        self.assertFalse((self.root / 'system/homing-relay.service').exists())
        self.assertFalse(self.load().get('active', False))
        self.save({})
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(self.load()['active'])
        self.assertTrue(self.load()['enabled'])

    def test_upgrade_failure_restores_link_and_activation(self):
        for active in (False, True):
            with self.subTest(active=active):
                if (self.root / 'base').exists(): shutil.rmtree(self.root / 'base')
                self.seed(active)
                self.assertNotEqual(self.run_install().returncode, 0)
                self.assertEqual((self.root / 'base/current').resolve().name, 'demo-old')
                self.assertEqual(self.load()['active'], active)
                self.assertTrue(self.load()['enabled'])

    def test_interrupted_copy_does_not_poison_revision(self):
        self.save({'interrupt_copy':True})
        self.assertNotEqual(self.run_install().returncode, 0)
        self.assertFalse((self.root / 'base/releases/demo-new/homing-server').exists())
        self.save({})
        result = self.run_install()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.root / 'base/current/homing-server').read_bytes(), self.binary.read_bytes())

if __name__ == '__main__': unittest.main()

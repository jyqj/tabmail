"""Explicit Go selection for runner children and syntax collectors.

R5_TEST_GO takes precedence over the legacy R5_GO selector. No downloads,
permission changes, or cache creation are performed here.
"""
import os
from pathlib import Path
import shutil


def selected(environment=None, *, fallback='go'):
    original = os.environ if environment is None else environment
    env = dict(original)
    name = original.get('R5_TEST_GO') or original.get('R5_GO') or fallback
    go = shutil.which(name, path=original.get('PATH')) or name
    go = str(Path(go).absolute())
    if original.get('R5_TEST_GO') or original.get('R5_GO'):
        # Do not inherit caller compiler/ABI switches into the selected tool.
        for key in ('GOEXPERIMENT', 'GOOS', 'GOARCH', 'GOAMD64', 'CGO_ENABLED',
                    'CC', 'CXX', 'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_CXXFLAGS',
                    'CGO_LDFLAGS', 'GODEBUG'):
            env.pop(key, None)
        env.update(GOENV='off', GOWORK='off', GOFLAGS='', GOTOOLCHAIN='local')
        env.update(R5_TEST_GO=go, R5_GO=go)
        env['PATH'] = str(Path(go).parent) + os.pathsep + original.get('PATH', '/usr/bin:/bin')
    for selector, target in (('R5_TEST_CACHE', 'GOCACHE'), ('R5_TEST_MODULECACHE', 'GOMODCACHE')):
        if original.get(selector):
            env[target] = original[selector]
    if original.get('R5_TEST_MODULECACHE'):
        env['GOPATH'] = str(Path(original['R5_TEST_MODULECACHE']).absolute().parent)
    return go, env

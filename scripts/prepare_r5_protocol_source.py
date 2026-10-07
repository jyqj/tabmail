"""Prepare the fixed CI shared-db source receipt; never certify protocol tests.

Uses the existing archive-v4 inventory and validator without changing either
policy. Receipts are fresh, private files outside the checkout. Failure never
falls back to Git HEAD, an older policy, or an existing receipt.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

import check_r5_protocol as protocol

inventory = protocol.source_inventory
POLICY = inventory.ARCHIVE_POLICY
GO_ENV_KEYS = ('GOVERSION', 'GOHOSTOS', 'GOHOSTARCH', 'GOOS', 'GOARCH',
               'CGO_ENABLED', 'GOENV', 'GOWORK', 'GOFLAGS')


def build_context(root):
    data = protocol.load_cases(root / 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json', root)
    tag_sets = sorted({tuple([adapter['build_tag']] if adapter.get('build_tag') else [])
                       for row in data['cases'] for adapter in row.get('shared_adapters', [])
                       if adapter.get('runner') != 'vitest' and 'db' in adapter['layers']})
    context = dict(goos='linux', goarch='amd64', cgo_enabled=1,
                   build_tag_sets=[list(tags) for tags in tag_sets], race=True,
                   go_work='off', go_flags='', selection='all_local_variants_superset')
    return inventory.validate_context(context, 'protocol')


def exclusive(path, raw):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(raw)


def prepare(root, output_dir):
    root, output_dir = Path(root).absolute(), Path(output_dir).absolute()
    if root != root.resolve() or output_dir != output_dir.resolve():
        raise ValueError('source and receipt paths must not traverse symlinks')
    if output_dir.is_relative_to(root) or root.is_relative_to(output_dir):
        raise ValueError('protocol receipt directory must be outside the checkout')
    if output_dir.exists() or output_dir.is_symlink():
        raise ValueError('protocol receipt directory must be fresh')

    context = build_context(root)
    # Reject ambient compiler injection with the original policy. This helper
    # does not remove unsupported overrides to make an unbound command pass.
    environment = inventory.bound_environment(context, os.environ)
    versions = re.findall(r'^go ([0-9]+\.[0-9]+\.[0-9]+)$', (root / 'go.mod').read_text(), re.M)
    if len(versions) != 1:
        raise ValueError('one exact repository Go version is required')
    expected_go = dict(GOVERSION='go' + versions[0], GOHOSTOS='linux', GOHOSTARCH='amd64',
                       GOOS=context['goos'], GOARCH=context['goarch'], CGO_ENABLED='1',
                       GOENV='', GOWORK='off', GOFLAGS='')
    command = ['go', 'env', '-json', *GO_ENV_KEYS]
    observed_go = inventory.strict_json(subprocess.check_output(
        command, cwd=root, env=environment, timeout=30))
    if observed_go != expected_go:
        raise ValueError('actual Go version/host/build context differs from the CI contract')

    receipt = inventory.capture_current_source(root, purpose='protocol', policy=POLICY,
                                               build_context=context)
    raw = inventory.canonical(receipt)
    digest = hashlib.sha256(raw).hexdigest()
    output_dir.mkdir(mode=0o700)
    manifest = output_dir / 'source-manifest.json'
    exclusive(manifest, raw)
    # Validate the file we wrote with its independently retained in-memory pin.
    # Only after this succeeds may the CI command read the published hash.
    verified = inventory.load_current_source(manifest, digest, root,
                                             purpose='protocol', policy=POLICY)
    exclusive(output_dir / 'source-manifest.sha256', (digest + '\n').encode())
    summary = dict(status='source_prepared', run='shared-db', source_policy=POLICY,
                   source_sha=verified['source_sha'], source_manifest_sha256=digest,
                   build_context=context, go_environment=observed_go,
                   task_complete=False, product_green=False,
                   boundary=verified['boundary'])
    exclusive(output_dir / 'preparation.json', (json.dumps(summary, indent=2) + '\n').encode())
    return summary


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args(argv)
    try:
        summary = prepare(args.root, args.output_dir)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        print('protocol source preparation failed: ' + type(error).__name__, file=sys.stderr)
        return 1
    print(json.dumps(summary, indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())

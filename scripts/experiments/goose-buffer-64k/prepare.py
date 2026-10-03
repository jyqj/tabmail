#!/usr/bin/env python3
"""Materialize fixed official source into a new owned experiment directory.

Never edits the product's go.mod, go.sum, forks or migrations. Do not put the
third-party trees, module zip, test binaries or database files into Git.
"""
import argparse
import base64
import difflib
import hashlib
import io
import json
from pathlib import Path
import shutil
import urllib.request
import zipfile

VERSION='v3.27.3'
MODULE='github.com/pressly/goose/v3'
URL=f'https://proxy.golang.org/{MODULE}/@v/{VERSION}.zip'
EXPECTED='h1:pIglVHjw99r4e/hDHHwbl9vfOsDMqUokfkXo6+n/RxA='
p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--repo',type=Path,required=True)
a=p.parse_args();r=a.root.resolve();repo=a.repo.resolve();r.mkdir(exist_ok=False,parents=True)
locks=(repo/'go.sum').read_text().splitlines();assert f'{MODULE} {VERSION} {EXPECTED}' in locks
payload=urllib.request.urlopen(URL).read();(r/'goose.zip').write_bytes(payload)
with zipfile.ZipFile(io.BytesIO(payload)) as z:
 # The Go module zip Hash1 domain: sorted full names, SHA-256(file) and newline.
 names=sorted(i.filename for i in z.infolist())
 lines=''.join(hashlib.sha256(z.read(n)).hexdigest()+'  '+n+'\n' for n in names)
 actual='h1:'+base64.b64encode(hashlib.sha256(lines.encode()).digest()).decode()
 assert actual==EXPECTED,(actual,EXPECTED)
 for arm in ('baseline','candidate'):
  tree=r/arm;tree.mkdir()
  for n in names:
   assert n.startswith(MODULE+'@'+VERSION+'/')
   rel=n[len(MODULE+'@'+VERSION+'/'):];assert '..' not in Path(rel).parts
   if not rel or n.endswith('/'):continue
   f=tree/rel;f.parent.mkdir(parents=True,exist_ok=True);f.write_bytes(z.read(n))
old=(r/'baseline/internal/sqlparser/parser.go').read_text()
assert old.count('buf := make([]byte, scanBufSize)')==1
new=old.replace('buf := make([]byte, scanBufSize)','buf := make([]byte, 64 * 1024)')
(r/'candidate/internal/sqlparser/parser.go').write_text(new)
(r/'goose-buffer.patch').write_text(''.join(difflib.unified_diff(old.splitlines(True),new.splitlines(True),fromfile='a/internal/sqlparser/parser.go',tofile='b/internal/sqlparser/parser.go')))
for arm in ('baseline','candidate'):
 mod=(repo/'go.mod').read_text().replace('=> ./third_party/','=> '+str(repo/'third_party')+'/')
 mod+='\nreplace '+MODULE+' '+VERSION+' => '+str(r/arm)+'\n'
 (r/(arm+'.mod')).write_text(mod);shutil.copyfile(repo/'go.sum',r/(arm+'.sum'))
 (r/(arm+'-modfile.patch')).write_text(''.join(difflib.unified_diff((repo/'go.mod').read_text().splitlines(True),mod.splitlines(True),fromfile='product/go.mod',tofile='experiment/'+arm+'.mod')))
json.dump({'official_registry_url':URL,'module':MODULE,'version':VERSION,'module_sum':actual,
 'zip_sha256':hashlib.sha256(payload).hexdigest(),'product_go_mod_sha256':hashlib.sha256((repo/'go.mod').read_bytes()).hexdigest(),
 'product_go_sum_sha256':hashlib.sha256((repo/'go.sum').read_bytes()).hexdigest()},(r/'registry-proof.json').open('x'),indent=2)
print('Prepared official source and one-line candidate; no product dependency eligibility granted.')

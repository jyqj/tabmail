#!/usr/bin/env python3
"""Read already-collected Go package metadata and bind selected source bytes."""
import argparse
import hashlib
import json
from pathlib import Path
p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--output',type=Path,required=True)
a=p.parse_args();r=a.root;o=a.output;o.mkdir(parents=True,exist_ok=True)
def sha(b):return hashlib.sha256(b).hexdigest()
decoder=json.JSONDecoder()
for arm in ('baseline','candidate'):
 text=(r/(arm+'-selected.json')).read_text();offset=0;packages=[]
 while offset<len(text):
  while offset<len(text) and text[offset].isspace():offset+=1
  if offset==len(text):break
  obj,end=decoder.raw_decode(text,offset);packages.append(obj);offset=end
 selected={};unknown=[];goose=[];replacements=[]
 for pkg in packages:
  if pkg.get('Error') or pkg.get('DepsErrors'):raise RuntimeError('metadata package errors')
  if pkg.get('ImportPath','').startswith('github.com/pressly/goose/v3'):
   assert pkg['Dir'].startswith(str(r/arm)),pkg['Dir']
   goose.append({k:pkg.get(k) for k in ['ImportPath','Dir','GoFiles','Module']})
  mod=pkg.get('Module',{})
  if 'Replace' in mod and mod not in replacements:replacements.append(mod)
  for key in ('GoFiles','CgoFiles','CFiles','CXXFiles','MFiles','HFiles','FFiles','SFiles','SysoFiles','EmbedFiles','TestGoFiles','XTestGoFiles','TestEmbedFiles','XTestEmbedFiles'):
   for name in pkg.get(key,[]):
    path=Path(name) if Path(name).is_absolute() else Path(pkg.get('Dir',''))/name
    if not path.is_file():unknown.append({'package':pkg.get('ImportPath'),'category':key,'path':str(path)});continue
    if str(path) in selected:continue
    b=path.read_bytes();selected[str(path)]={'path':str(path),'bytes':len(b),'sha256':sha(b)}
 rows=sorted(selected.values(),key=lambda x:x['path'])
 record={'arm':arm,'collected_after_compilation':True,'package_records':len(packages),
 'selected_file_records':len(rows),'source_binding_sha256':sha(json.dumps(rows,sort_keys=True).encode()),
 'goose_packages':goose,'local_replacements':replacements,'selected_files':rows,'unknown_files':unknown,
 'binary_sha256':sha((r/(arm+'-pg.test')).read_bytes()),
 'qualification':'selected bytes and actual resolved directories only; generated/native/compiler qualification not granted'}
 (o/(arm+'-source-binding.json')).write_text(json.dumps(record,indent=2)+'\n')
 print(arm,'selected',len(rows),'goose packages',len(goose),'unknown',len(unknown))

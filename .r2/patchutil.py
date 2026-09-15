from pathlib import Path
import re

def read(path):
    return Path(path).read_text()

def write(path, text):
    p=Path(path); p.parent.mkdir(parents=True,exist_ok=True); p.write_text(text.strip()+"\n")

def replace(path, old, new, count=1):
    s=read(path)
    assert s.count(old)==count, (path,old[:120],s.count(old),count)
    Path(path).write_text(s.replace(old,new))

def function(path, signature, body):
    s=read(path); start=s.index(signature); end=re.search(r'^}',s[start:],re.M)
    assert end,signature
    Path(path).write_text(s[:start]+body.strip()+s[start+end.end():])

def imports(path,*packages):
    s=read(path)
    for p in packages:
        if '"'+p+'"' not in s:
            assert 'import (' in s,path
            s=s.replace('import (','import (\n\t"'+p+'"',1)
    Path(path).write_text(s)

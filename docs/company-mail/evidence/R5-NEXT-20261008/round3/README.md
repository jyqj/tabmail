# Round 3 evidence

The ten-task product source is `ff8872b737a947c158035a0b07d78396397ad626`, tree `87d50267a34657765f666594aa56195539bccfc3`, identical to tested local `db7702d`.

`summary.json` records the distinct source scopes, commands, results, limitations and file hashes. Direct JSONL/JSON/logs are readable without unpacking. The original formal timeouts and failure gates are unchanged.

## Complete byte archives

ZIP archives are carried as standard base64 text so the GitHub text-tree transport can preserve every byte, including invalid UTF-8 control outputs. Each `encoded_file` is tracked; `decoded_archive_bytes` and `decoded_archive_sha256` describe the exact ZIP reconstructed from it. This encoding changes no archived member bytes.

From the repository root, decode the three archives:

```sh
python3 - <<'PYCODE'
import base64, hashlib, json
from pathlib import Path
root = Path('docs/company-mail/evidence/R5-NEXT-20261008')
summary = json.loads((root / 'round3/summary.json').read_text())
for name, record in summary['archives'].items():
    encoded = root / 'round3' / record['encoded_file']
    raw = base64.b64decode(b''.join(encoded.read_bytes().splitlines()), validate=True)
    assert len(raw) == record['decoded_archive_bytes']
    assert hashlib.sha256(raw).hexdigest() == record['decoded_archive_sha256']
    (root / 'round3' / name).write_bytes(raw)
PYCODE
```

- `08-raw-evidence.zip`: all 244 files listed in `08-evidence-inventory.json`, including the actual owned child programs, raw expected/observed streams, reports, execution records, red and green controls. The 0.75-second Python controls are synthetic Go-event/source fixtures, not a real Go/PG or formal 180-second run.
- `09-raw-evidence.zip`: all 12 UI author records listed in `09-evidence-inventory.json`, including the initial frozen 25 tests, first candidate source, the additional one-case failing counterexample and final 35-case result. The filtered diagnostic skips are not final-run skips.
- `../round2/author-raw-supplement.zip`: the 18 deeper raw-output references identified by independent review. `../round2/author-raw-supplement.json` maps each original workspace-relative path to its archive member and pins every byte. Existing round2 author summaries and primary evidence remain unchanged.

The original 171 parent checkboxes remain 10 accepted and 161 outstanding. Archive completion, source-catalog maintenance and review are not additional implementation tasks.

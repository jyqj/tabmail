# Actual round2 integration CI

[Run 37654826526](https://github.com/jyqj/tabmail/actions/runs/37654826526) is associated with head `b916bee08399eacd199d3bef8e5e5ebc2265a7d8`. Its actual tested checkout is GitHub's synthetic merge `93889b26ca0e0cafbe0f58326804c7c44552cbee`; both identities are retained. It predates round3 (#82) and revision6.

The original downloaded backend artifact is preserved exactly as base64. Its byte count and SHA-256 match the GitHub artifact metadata in `archive.json`. Decode it with Python's `base64.b64decode(b''.join(path.read_bytes().splitlines()), validate=True)` and verify `decoded_bytes`/`decoded_sha256` before reading the ZIP. `backend-artifact-inventory.json` pins its extracted original members.

Root independently checked the official merge identity, GitHub artifact digest and actual protocol JSONL: 19 top-level tests / 88 distinct leaf tests passed, zero failures or skips, package32.31s, real PostgreSQL, exit0. The archive-v4 protocol manifest has 1,229 entries and its actual hash equals the independently supplied pin; before/after source closure equals the same manifest. This confirms task05 CI wiring reached real protocol execution.

The whole backend remains failed. The PG package ended after171.1s with737 failed test names and105 skips; these are not leaf-case counts. The separate mandatory-execution gate remains failed. Current source tests ran733 with three catalog/compatibility/transaction errors; frozen historical tests ran4 successfully. These values are newly read from this run, not copied from prior180-second failures. The report's scoped protocol product_green is not complete product/CI/M/G0 qualification, and a successful run predating08 does not prove its timeout/start-error path.

Frontend job failure, dependency-audit failure, and production/browser success are recorded as actual job/step outcomes. No frontend artifact was downloaded for this record, so it does not assert a new per-test frontend denominator or vulnerability count.

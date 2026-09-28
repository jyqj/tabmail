#!/usr/bin/env python3
"""Run opt-in pending regressions; require exact target failures, not bad setup.

This command's success means BASELINE DEFECTS REPRODUCED, never product fixed.
It does not access .env or create infrastructure. Supply only a disposable DSN.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
EXPECTED = {
 "TestR5AuditA01OverridePreservesUneditedRestriction": "A01",
 "TestR5AuditA02StaleProfileCannotRestoreRevocation": "A02",
 "TestR5AuditA03ExpiredContentDeniedAcrossEntrypoints": "A03",
 "TestR5AuditA04FrozenEmployeeCanBeHandedOver": "A04",
 "TestR5AuditA05RestorePreservesHardExpiry": "A05",
 "TestR5AuditA06ProtectedPrefixDoesNotStarveGC": "A06",
 "TestR5AuditA07SentAssetKeepsBCCAfterQueueCleanup": "A07",
}

def classify(text: str, exit_code: int) -> dict:
    errors = []
    events = [json.loads(line) for line in text.splitlines() if line.strip()]
    states = {test: [] for test in EXPECTED}
    outputs = {test: "" for test in EXPECTED}
    package_failed = 0
    for event in events:
        if not isinstance(event, dict) or event.get("Package") != "tabmail/internal/store/postgres":
            errors.append("unexpected event/package"); continue
        action, test = event.get("Action"), event.get("Test")
        if test:
            if test not in states:
                errors.append(f"unexpected test {test}"); continue
            if action == "output": outputs[test] += event.get("Output", "")
            elif action in {"run", "pass", "fail", "skip"}: states[test].append(action)
            else: errors.append(f"unexpected lifecycle {action}")
        elif action == "fail": package_failed += 1
    if exit_code != 1: errors.append(f"expected failing Go exit 1, got {exit_code}")
    if package_failed != 1: errors.append("missing/duplicate failed package completion")
    reproduced = []
    for test, code in EXPECTED.items():
        marker = f"R5_BASELINE_DEFECT_{code}:"
        if states[test] != ["run", "fail"] or outputs[test].count(marker) != 1:
            errors.append(f"{code}: missing exact target assertion failure; setup/skip/pass is not reproduction")
        else: reproduced.append(code)
    return {"status": "baseline_reproduced" if not errors else "invalid_baseline_evidence", "product_fixed": False,
            "reproduced": reproduced, "errors": errors, "process_exit_code": exit_code,
            "log_sha256": hashlib.sha256(text.encode()).hexdigest(), "tests_expected": len(EXPECTED)}

def main() -> int:
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--output-dir", required=True, type=Path)
    p.add_argument("--source-sha", required=True)
    args = p.parse_args()
    if len(args.source_sha) != 40 or any(x not in "0123456789abcdef" for x in args.source_sha): p.error("exact commit SHA required")
    if not os.environ.get("TABMAIL_TEST_DB_DSN"): p.error("disposable TABMAIL_TEST_DB_DSN required")
    args.output_dir.mkdir(parents=True, exist_ok=False)
    command = ["go", "test", "-json", "-race", "-count=1", "-timeout=180s", "-tags=r5audit", "-run", "^TestR5AuditA0[1-7]", "./internal/store/postgres"]
    result = subprocess.run(command, cwd=ROOT, capture_output=True, text=True, timeout=240)
    (args.output_dir / "audit.jsonl").write_text(result.stdout)
    (args.output_dir / "audit.stderr").write_text(result.stderr)
    report = classify(result.stdout, result.returncode)
    report.update(source_sha=args.source_sha, command=command,
                  test_source_sha256=hashlib.sha256((ROOT / "internal/store/postgres/r5_audit_baseline_test.go").read_bytes()).hexdigest(),
                  evidence_boundary="Real PostgreSQL and loopback HTTP; deterministic fixtures. A01/A02 UI component integration still separate.")
    (args.output_dir / "report.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))
    return 0 if not report["errors"] else 1

if __name__ == "__main__":
    try: raise SystemExit(main())
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        print(f"baseline driver failed: {type(exc).__name__}: {exc}", file=sys.stderr); raise SystemExit(1)

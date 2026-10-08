"""Synthetic verifier checks, not evidence of PostgreSQL execution."""
import json
from pathlib import Path
import tempfile
import unittest

from scripts import run_r5_api_key_issuance as runner


class APIKeyIssuanceVerifierTest(unittest.TestCase):
    def events(self, baseline):
        events = []
        for name in sorted(runner.POSITIVE | runner.NEGATIVE | {runner.PARENT}):
            events.append({"Package": runner.PACKAGE, "Action": "run", "Test": name})
            if baseline and name in runner.NEGATIVE:
                if "/holds_" in name:
                    message = "API-key issuer did not fence " + name.split("/holds_", 1)[1] + " before INSERT: SQLSTATE=none"
                elif name.endswith("/required_audit_rollback"):
                    message = "stale or failed API-key issuance escaped: status=201 want=500 counts=[1 1 0] secret_returned=true"
                else:
                    message = "stale or failed API-key issuance escaped: status=201 want=403 counts=[1 1 1] secret_returned=true"
                events.append({"Package": runner.PACKAGE, "Action": "output", "Test": name, "Output": message})
            fail = baseline and name in runner.NEGATIVE | {runner.PARENT}
            events.append({"Package": runner.PACKAGE, "Action": "fail" if fail else "pass", "Test": name})
        events.append({"Package": runner.PACKAGE, "Action": "fail" if baseline else "pass"})
        return events

    def verify(self, events, baseline, code=None):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "synthetic.jsonl"
            log.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            return runner.verify(log, int(baseline) if code is None else code, baseline)

    def test_accepts_only_complete_declared_execution(self):
        for baseline in (False, True):
            with self.subTest(baseline=baseline):
                result = self.verify(self.events(baseline), baseline)
                self.assertEqual(result["leaf_count"], 18)
                self.assertEqual(result["passed"], 6 if baseline else 18)

    def test_rejects_missing_duplicate_and_skipped_results(self):
        for mutation in ("missing", "duplicate", "skip", "exit"):
            with self.subTest(mutation=mutation):
                events = self.events(False)
                if mutation == "missing":
                    events.pop(0)
                elif mutation == "duplicate":
                    events.append(events[0])
                elif mutation == "skip":
                    next(event for event in events if event["Action"] == "pass")["Action"] = "skip"
                with self.assertRaises(ValueError):
                    self.verify(events, False, 1 if mutation == "exit" else 0)

    def test_baseline_http_failure_is_not_a_stale_issuance(self):
        for mutation in ("status", "rows", "secret", "lock_error", "unfinished_issue"):
            with self.subTest(mutation=mutation):
                events = self.events(True)
                if mutation in ("lock_error", "unfinished_issue"):
                    event = next(event for event in events if event.get("Output", "").startswith("API-key issuer did not fence"))
                    if mutation == "lock_error":
                        event["Output"] = event["Output"].replace("SQLSTATE=none", "SQLSTATE=non-PG-error")
                    else:
                        event["Output"] += "\nnormal key issue lost atomic result: status=500 counts=[0 0 0]"
                else:
                    event = next(event for event in events if "want=403" in event.get("Output", ""))
                    replacements = {"status": ("status=201", "status=500"), "rows": ("counts=[1 1 1]", "counts=[0 0 0]"), "secret": ("secret_returned=true", "secret_returned=false")}
                    event["Output"] = event["Output"].replace(*replacements[mutation])
                with self.assertRaises(ValueError):
                    self.verify(events, True)


if __name__ == "__main__":
    unittest.main()

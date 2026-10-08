"""A red baseline is evidence only when the stale-write defect was observed."""
import json
from pathlib import Path
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import run_r5_webhook_claim_completion as gate


class WebhookClaimEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="r5-webhook-gate-")
        self.addCleanup(self.temp.cleanup)
        self.log = Path(self.temp.name) / "events.jsonl"

    def events(self):
        positive, negative = gate.expected()
        names = set(positive | negative)
        for name in tuple(names):
            parts = name.split("/")
            names.update("/".join(parts[:index]) for index in range(1, len(parts)))
        rows = [{"Package": gate.PACKAGE, "Test": name, "Action": "run"} for name in sorted(names)]
        for name in sorted(names):
            failed = name in negative or any(leaf.startswith(name + "/") for leaf in negative)
            rows.append({"Package": gate.PACKAGE, "Test": name, "Action": "fail" if failed else "pass"})
        rows.append({"Package": gate.PACKAGE, "Action": "fail"})
        return rows

    def write(self, rows):
        self.log.write_text("".join(json.dumps(row) + "\n" for row in rows))

    def test_fixture_failure_with_the_expected_failed_ids_is_not_a_defect_baseline(self):
        rows = self.events()
        rows += [{"Package": gate.PACKAGE, "Test": name, "Action": "output", "Output": "database connection failed\n"}
                 for name in gate.expected()[1]]
        self.write(rows)
        with self.assertRaisesRegex(ValueError, "without the required stale-write"):
            gate.verify(self.log, 1, baseline=True)

    def test_incomplete_or_skipped_execution_never_qualifies(self):
        for fault in ("missing", "skip", "build-failure"):
            with self.subTest(fault=fault):
                rows = self.events()
                if fault == "missing":
                    rows.pop(0)
                elif fault == "skip":
                    next(row for row in rows if row["Action"] == "fail" and row.get("Test"))["Action"] = "skip"
                else:
                    rows = [{"Package": gate.PACKAGE, "Action": "fail"}]
                self.write(rows)
                with self.assertRaises(ValueError):
                    gate.verify(self.log, 1, baseline=True)

    def test_malformed_event_log_is_rejected(self):
        self.log.write_text("not a Go event\n")
        with self.assertRaises(json.JSONDecodeError):
            gate.verify(self.log, 1, baseline=True)


if __name__ == "__main__":
    unittest.main()

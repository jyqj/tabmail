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
        names.update(name.split("/", 1)[0] for name in tuple(names))
        rows = [{"Package": gate.PACKAGE, "Test": name, "Action": "run"} for name in sorted(names)]
        for name in sorted(names):
            failed = name in negative or any(leaf.startswith(name + "/") for leaf in negative)
            rows.append({"Package": gate.PACKAGE, "Test": name, "Action": "fail" if failed else "pass"})
        rows.append({"Package": gate.PACKAGE, "Action": "fail"})
        return rows

    def defect_events(self):
        rows = self.events()
        observations = {
            "RejectsLostOwnership": "lost ownership did not return ErrClaimLeaseLost: <nil>",
            "PreservesNewerTerminalState": "stale completion did not report lost generation: <nil>",
            "ChecksLeaseAfterRowWait": "completion after lease expiry was not fenced: <nil>",
        }
        for name in gate.expected()[1]:
            family = name.split("/", 1)[0].removeprefix("TestR5WebhookClaimCompletion")
            rows.append({"Package": gate.PACKAGE, "Test": name, "Action": "output",
                         "Output": observations[family] + "\n"})
        return rows

    def test_direct_subtest_names_with_slashes_have_no_phantom_parent_execution(self):
        # Mirrors the frozen source's direct t.Run("kind/boundary") calls and
        # the actual first PostgreSQL run, not nested t.Run("kind") calls.
        rows = self.defect_events()
        self.assertEqual(sum(row["Action"] == "run" for row in rows), 83)
        self.write(rows)
        result = gate.verify(self.log, 1, baseline=True)
        self.assertEqual((result["passed"], result["failed"], result["skipped"]), (15, 63, 0))
        for row in rows:
            if row["Action"] == "fail":
                row["Action"] = "pass"
        self.write(rows)
        self.assertEqual(gate.verify(self.log, 0, baseline=False)["passed"], 78)

    def test_extra_intermediate_execution_is_rejected(self):
        rows = self.defect_events()
        phantom = "TestR5WebhookClaimCompletionRejectsLostOwnership/outbox-done"
        rows += [{"Package": gate.PACKAGE, "Test": phantom, "Action": action}
                 for action in ("run", "fail")]
        self.write(rows)
        with self.assertRaisesRegex(ValueError, "missing or unexpected"):
            gate.verify(self.log, 1, baseline=True)

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

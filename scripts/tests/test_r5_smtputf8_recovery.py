"""Synthetic checker controls; these are not SMTP delivery tests."""
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location(
    "smtputf8_recovery_runner", Path(__file__).resolve().parents[1] / "run_r5_smtputf8_recovery.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class SMTPUTF8RecoveryVerifierTest(unittest.TestCase):
    def events(self, baseline):
        events = []
        for name in sorted(runner.LEAVES | runner.PARENTS):
            events.append({"Package": runner.PACKAGE, "Action": "run", "Test": name})
            if baseline and name in runner.NEGATIVE:
                messages = [
                    "SMTPUTF8 requirement must produce a local pre-send error, got <nil>",
                    runner.MARKER + ' MAIL=["MAIL FROM:<sender@example.test>"] RCPT=["RCPT TO:<reader@example.test>"] DATA=1 body_bytes=128',
                ]
                if name == runner.NEXT_MX:
                    messages += ['SMTPUTF8 mismatch must try the next MX before any DATA: visited=["first.test"]']
                elif name.startswith(runner.LEDGER + "/"):
                    messages += [
                        "actual delivery recipient state = accepted, want temporary: <nil>",
                        "job sent state disagrees with actual peer capability: capable=false job=sent err=<nil>",
                        "capability check fabricated a server rejection or acceptance: attempts=250 err=<nil>",
                    ]
                for message in messages:
                    events.append({"Package": runner.PACKAGE, "Action": "output", "Test": name,
                                   "Output": "    r5_exec_smtputf8_test.go:243: " + message + "\n"})
            failed = baseline and name in runner.NEGATIVE | runner.PARENTS
            events.append({"Package": runner.PACKAGE, "Action": "fail" if failed else "pass", "Test": name})
        events.append({"Package": runner.PACKAGE, "Action": "fail" if baseline else "pass"})
        return events

    def verify(self, events, baseline, code=None):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "synthetic.jsonl"
            log.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            return runner.verify(log, int(baseline) if code is None else code, baseline)

    def test_accepts_exact_baseline_71_pass_38_fail_and_candidate_109_pass(self):
        for baseline in (True, False):
            with self.subTest(baseline=baseline):
                result = self.verify(self.events(baseline), baseline)
                self.assertEqual(result["leaf_count"], 109)
                self.assertEqual(result["passed"], 71 if baseline else 109)
                self.assertEqual(result["failed"], 38 if baseline else 0)
                self.assertEqual(len(result["actual_wire_observations"]), 38 if baseline else 0)

    def test_rejects_missing_duplicate_skipped_and_unfinished_execution(self):
        for baseline in (True, False):
            for mutation in ("missing_run", "duplicate_run", "missing_terminal", "duplicate_terminal", "skip", "new_test"):
                with self.subTest(baseline=baseline, mutation=mutation):
                    events = self.events(baseline)
                    terminal = next(e for e in events if e["Action"] in ("pass", "fail"))
                    if mutation == "missing_run":
                        events.pop(0)
                    elif mutation == "duplicate_run":
                        events.append(copy.deepcopy(events[0]))
                    elif mutation == "missing_terminal":
                        events.remove(terminal)
                    elif mutation == "duplicate_terminal":
                        events.append(copy.deepcopy(terminal))
                    elif mutation == "skip":
                        terminal["Action"] = "skip"
                    else:
                        events.append({"Package": runner.PACKAGE, "Action": "run", "Test": "Unexpected"})
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline)

    def test_rejects_wrong_failure_set_or_exit_code(self):
        for baseline in (True, False):
            for target in (sorted(runner.POSITIVE)[0], sorted(runner.NEGATIVE)[0], runner.CAPABILITIES, ""):
                with self.subTest(baseline=baseline, target=target):
                    events = self.events(baseline)
                    terminal = next(e for e in events if e.get("Test", "") == target and e["Action"] in ("pass", "fail"))
                    terminal["Action"] = "pass" if terminal["Action"] == "fail" else "fail"
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline)
            with self.assertRaises(ValueError):
                self.verify(self.events(baseline), baseline, 0 if baseline else 1)

    def test_rejects_absent_fake_duplicate_or_reassigned_wire_evidence(self):
        for mutation in ("missing", "duplicate", "missing_mail", "missing_recipient", "no_data", "empty_body", "wrong_scope"):
            with self.subTest(mutation=mutation):
                events = self.events(True)
                event = next(e for e in events if runner.MARKER in e.get("Output", ""))
                if mutation == "missing":
                    events.remove(event)
                elif mutation == "duplicate":
                    events.append(copy.deepcopy(event))
                elif mutation == "wrong_scope":
                    event["Test"] = sorted(runner.POSITIVE)[0]
                else:
                    old, new = {
                        "missing_mail": ('MAIL=["MAIL FROM:<sender@example.test>"]', "MAIL=[]"),
                        "missing_recipient": ('RCPT=["RCPT TO:<reader@example.test>"]', "RCPT=[]"),
                        "no_data": ("DATA=1", "DATA=0"),
                        "empty_body": ("body_bytes=128", "body_bytes=0"),
                    }[mutation]
                    event["Output"] = event["Output"].replace(old, new)
                with self.assertRaises(ValueError):
                    self.verify(events, True)

    def test_rejects_extra_assertions_in_any_scope_and_duplicate_allowed_assertions(self):
        for baseline in (True, False):
            for scope in (sorted(runner.NEGATIVE)[0], sorted(runner.POSITIVE)[0], runner.CAPABILITIES, ""):
                with self.subTest(baseline=baseline, scope=scope):
                    events = self.events(baseline)
                    events.append({"Package": runner.PACKAGE, "Action": "output", "Test": scope,
                                   "Output": "    fixture.go:20: owned peer did not stop\n"})
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline)
        events = self.events(True)
        events.append(copy.deepcopy(next(e for e in events if "local pre-send error" in e.get("Output", ""))))
        with self.assertRaises(ValueError):
            self.verify(events, True)

    def test_rejects_empty_unrelated_build_race_or_panic_execution(self):
        for mutation in ("empty", "package", "build", "failed_build", "race", "panic"):
            with self.subTest(mutation=mutation):
                events = self.events(True)
                if mutation == "empty":
                    events = []
                elif mutation == "package":
                    events[0]["Package"] = "tabmail/internal/unrelated"
                elif mutation == "build":
                    events.append({"Package": runner.PACKAGE, "Action": "build-fail"})
                elif mutation == "failed_build":
                    events[-1]["FailedBuild"] = runner.PACKAGE
                else:
                    events.append({"Package": runner.PACKAGE, "Action": "output",
                                   "Output": "WARNING: DATA RACE\n" if mutation == "race" else "panic: failure\n"})
                with self.assertRaises(ValueError):
                    self.verify(events, True)


if __name__ == "__main__":
    unittest.main()

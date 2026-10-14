"""Synthetic execution-checker guards; these do not prove PG behavior."""
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location(
    "settings_seed_runner", Path(__file__).resolve().parents[1] / "run_r5_settings_seed.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class SettingsSeedVerifierTest(unittest.TestCase):
    def events(self, baseline):
        events = []
        for name in sorted(runner.POSITIVE | runner.NEGATIVE | runner.PARENTS):
            events.append({"Package": runner.PACKAGE, "Action": "run", "Test": name})
            if baseline and name in runner.NEGATIVE:
                writer = name.rsplit("/", 1)[1]
                message = ('    r5_settings_seed_pg_test.go:145: ' + runner.MARKER +
                           ' writer=' + writer + ' before_value="false" after_value="true" '
                           'before_description="' + writer + ' committed choice" '
                           'after_description="first startup environment" '
                           'before_updated_at=2026-10-09T03:00:00Z after_updated_at=2026-10-09T03:00:01Z\n')
                events.append({"Package": runner.PACKAGE, "Action": "output", "Test": name, "Output": message})
            fail = baseline and name in runner.NEGATIVE | {runner.CONCURRENT}
            events.append({"Package": runner.PACKAGE, "Action": "fail" if fail else "pass", "Test": name})
        events.append({"Package": runner.PACKAGE, "Action": "fail" if baseline else "pass"})
        return events

    def verify(self, events, baseline, code=None):
        with tempfile.TemporaryDirectory() as directory:
            log = Path(directory) / "synthetic.jsonl"
            log.write_text("\n".join(json.dumps(event) for event in events) + "\n")
            return runner.verify(log, int(baseline) if code is None else code, baseline)

    def test_accepts_exact_baseline_six_pass_two_fail_and_candidate_eight_pass(self):
        for baseline in (True, False):
            with self.subTest(baseline=baseline):
                result = self.verify(self.events(baseline), baseline)
                self.assertEqual(result["leaf_count"], 8)
                self.assertEqual(result["passed"], 6 if baseline else 8)
                self.assertEqual(result["failed"], 2 if baseline else 0)
                self.assertEqual(len(result["actual_overwrite_observations"]), 2 if baseline else 0)

    def test_rejects_missing_duplicate_unfinished_and_skipped_results(self):
        for baseline in (True, False):
            for mutation in ("missing_run", "duplicate_run", "missing_terminal", "duplicate_terminal", "skip"):
                with self.subTest(baseline=baseline, mutation=mutation):
                    events = self.events(baseline)
                    if mutation == "missing_run":
                        events.pop(0)
                    elif mutation == "duplicate_run":
                        events.append(events[0])
                    else:
                        terminal = next(e for e in events if e["Action"] in ("pass", "fail"))
                        if mutation == "missing_terminal":
                            events.remove(terminal)
                        elif mutation == "duplicate_terminal":
                            events.append(terminal)
                        else:
                            terminal["Action"] = "skip"
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline)

    def test_rejects_wrong_failure_set_and_exit_status(self):
        for baseline in (True, False):
            for mutation in ("parent", "positive", "negative", "package", "exit"):
                with self.subTest(baseline=baseline, mutation=mutation):
                    events = self.events(baseline)
                    target = {"parent": runner.CONCURRENT, "positive": sorted(runner.POSITIVE)[0],
                              "negative": sorted(runner.NEGATIVE)[0], "package": ""}
                    if mutation != "exit":
                        terminal = next(e for e in events if e.get("Test", "") == target[mutation]
                                        and e["Action"] in ("pass", "fail"))
                        terminal["Action"] = "pass" if terminal["Action"] == "fail" else "fail"
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline, 0 if baseline else 1) if mutation == "exit" else self.verify(events, baseline)

    def test_rejects_connection_metadata_or_cleanup_failure_as_overwrite_evidence(self):
        replacements = {
            "writer": ("writer=concurrent_admin_write", "writer=unrelated"),
            "winner_value": ('before_value="false"', 'before_value=""'),
            "stale_value": ('after_value="true"', 'after_value="false"'),
            "winner_description": ("concurrent_admin_write committed choice", "not committed"),
            "stale_description": ("first startup environment", "some other value"),
            "timestamp": ("after_updated_at=2026-10-09T03:00:01Z", "after_updated_at=missing"),
            "missing_marker": (runner.MARKER, "connection failed"),
        }
        for mutation in (*replacements, "extra_assertion", "duplicate_marker", "wrong_test"):
            with self.subTest(mutation=mutation):
                events = self.events(True)
                event = next(e for e in events if e["Action"] == "output")
                if mutation in replacements:
                    event["Output"] = event["Output"].replace(*replacements[mutation])
                elif mutation == "extra_assertion":
                    event["Output"] += "    fixture.go:61: owned PostgreSQL database cleanup failed: error\n"
                elif mutation == "duplicate_marker":
                    event["Output"] += event["Output"]
                else:
                    event["Test"] = runner.PRESERVE
                with self.assertRaises(ValueError):
                    self.verify(events, True)

    def test_rejects_unrelated_package_build_race_and_panic_events(self):
        for mutation in ("empty", "package", "new_test", "build", "failed_build", "race", "panic"):
            with self.subTest(mutation=mutation):
                events = self.events(True)
                if mutation == "empty":
                    events = []
                elif mutation == "package":
                    events[0]["Package"] = "tabmail/internal/unrelated"
                elif mutation == "new_test":
                    events.append({"Package": runner.PACKAGE, "Action": "run", "Test": "Unexpected"})
                elif mutation == "build":
                    events.append({"Package": runner.PACKAGE, "Action": "build-fail"})
                elif mutation == "failed_build":
                    events[-1]["FailedBuild"] = runner.PACKAGE
                else:
                    output = "WARNING: DATA RACE\n" if mutation == "race" else "panic: unexpected failure\n"
                    events.append({"Package": runner.PACKAGE, "Action": "output", "Output": output})
                with self.assertRaises(ValueError):
                    self.verify(events, True)

    def test_rejects_source_assertions_outside_the_two_negative_leaves(self):
        for baseline in (True, False):
            for scope in (runner.CONCURRENT, "", sorted(runner.POSITIVE)[0]):
                with self.subTest(baseline=baseline, scope=scope):
                    events = self.events(baseline)
                    events.append({"Package": runner.PACKAGE, "Action": "output", "Test": scope,
                                   "Output": "    fixture.go:61: owned PostgreSQL database cleanup failed: error\n"})
                    with self.assertRaises(ValueError):
                        self.verify(events, baseline)


if __name__ == "__main__":
    unittest.main()

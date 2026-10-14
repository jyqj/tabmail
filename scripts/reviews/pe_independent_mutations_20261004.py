"""Independent fail-closed controls; restore exact test bytes after every run.

Requires an explicitly supplied owned PG DSN and private evidence directory.
Mutates only test adapters/observer, never production. Do not run concurrently
with another task using these source files. This is not formal qualification.
"""
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
SHARED = ROOT / "web/components/company/r5-protocol-shared-components.test.tsx"
OBSERVER = ROOT / "web/components/company/r5-streaming-fetch-observer.ts"


def main():
    if not os.environ.get("TABMAIL_TEST_DB_DSN"):
        raise SystemExit("Explicit owned PG DSN required")
    evidence = Path(os.environ["TABMAIL_PE_FOCUSED_EVIDENCE"])
    evidence.mkdir(parents=True, exist_ok=True)
    controls = [
        ("raw_loss", SHARED, '    assertPermissionIntent(id, fixture.variant, before, command, after);',
         '    if (after.overrides) { after.overrides.can_send = null; after.field_sources.can_send = after.profile ? "profile" : "default"; }\n    assertPermissionIntent(id, fixture.variant, before, command, after);', "PE01", "persisted raw scalar"),
        ("accepted_cas", SHARED, '      expected_revision: observed.revision,\n      fields: { description: "Shared stale description", can_send: observed.can_send,',
         '      expected_revision: revoked.revision,\n      fields: { description: "Shared stale description", can_send: observed.can_send,', "PE03", "resolved"),
        ("wrong_event", OBSERVER, 'action: metadata.action, resource_type: metadata.resource_type, resource_id: metadata.resource_id',
         'action: metadata.action === "permission.profile.update" ? "permission.profile.delete" : metadata.action, resource_type: metadata.resource_type, resource_id: metadata.resource_id', "PE03", "expected false to be true"),
    ]
    outcomes = []
    for name, path, needle, replacement, case, diagnostic in controls:
        original = path.read_bytes()
        source = original.decode()
        if source.count(needle) != 1:
            raise RuntimeError(f"Source binding failed: {name}")
        try:
            path.write_text(source.replace(needle, replacement))
            result = subprocess.run(["go", "test", "-mod=readonly", "-count=1", "-tags=r5protocol", "-timeout=180s",
                                     "./internal/api/handlers", "-run", f"^TestPESharedAdapterFocused$/{case}", "-v"],
                                    cwd=ROOT, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=190)
            (evidence / f"mutation-{name}.log").write_bytes(result.stdout)
            component_report = json.loads((evidence / f"{case}-default-vitest.json").read_text())
            failures = "\n".join(message for suite in component_report["testResults"]
                                 for assertion in suite["assertionResults"] for message in assertion["failureMessages"])
            rejected = result.returncode != 0 and diagnostic in failures
            outcomes.append({"control": name, "case": case, "exit_code": result.returncode,
                             "expected_assertion_rejected": rejected})
            if not rejected:
                raise RuntimeError(f"Negative control did not fail for expected assertion: {name}")
        finally:
            path.write_bytes(original)
            assert path.read_bytes() == original
    (evidence / "independent-mutations.json").write_text(json.dumps(outcomes, indent=2) + "\n")
    print(json.dumps(outcomes, indent=2))


if __name__ == "__main__":
    main()

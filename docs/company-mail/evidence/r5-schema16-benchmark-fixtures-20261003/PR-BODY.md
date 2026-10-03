Fix synthetic schema16 pipeline fixtures that used the current schema19 migration version and failed during setUp before reaching their intended assertions. Scope the modeled schema16 source to the two grammar test classes, retain the production validator and all original negative cases, and add explicit schema18/19 rejection and downstream checkpoint regressions.

Make the two mocked CLI capacity fixtures independent of small /tmp mounts while still running the real preflight, with an exact disk-margin regression. No production, lock, inventory, descriptor, central TODO or historical evidence changes.

Validation: benchmark file 111/111; current18 benchmark 21/21; archive boundary 15/15; CI wiring 5/5; version runner 4/4. Initial 106-test FAIL and intermediate failures retained in docs/company-mail/evidence/r5-schema16-benchmark-fixtures-20261003. No runtime benchmark or full integration qualification; no merge/deploy.

Base: integration/company-mail-r5-batch4-20261003. Fixed source: aeed47f0e1998d9925acb680992b051463821d69.

# Company mail safety and interaction contracts

Implementation baseline: `fc111874efb835bd98215f1841137736c92a9cce`.

This change set addresses domain asset deletion protection, published-template
eligibility and pinned draft editing, compose identity selection, revocation-safe
content rendering, and company-domain cache consistency. Security-sensitive
administrative writes are reviewed for optimistic concurrency.

The existing atomic draft submission, recipient delivery ledger, current content
authorization and recovery mechanisms remain authoritative. No production data,
DNS, deployment, or external mail delivery is modified by this pull request.

Implementation and validation results will be recorded here with the completed
changes; the presence of this document alone is not evidence that a fix passed.

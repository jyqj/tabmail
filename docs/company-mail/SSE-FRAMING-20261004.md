# Client SSE framing: bounded R5-P7-090 progress

## Source and scope

- Baseline: `593bbc6a3ac91b5dbf8917602e45900bff094e27` (PR #39).
- Tested/reviewed local source: `6b180821a397451d0837a536f38816a615ec6f54`.
- Published equivalent source: `ba93e24dd2eb498798e97e75e9626f8d1f3a1e41`.
- Both source commits have exactly the same complete Git tree: `81be69dcb208428529e70ea4d2001c06e4be1a09`. The later delivery changes only this report and the targeted TODO progress note.
- This is a client invalidation/reconnect correctness slice. R5-P7-090, its dependencies, and the 171-task parent remain open; the central accepted count stays **10/171**, and historical F52 remains failed.

## Product behavior fixed

The old transport normalized CRLF separately in each decoded network chunk. Splitting CR and LF across chunks could leave a complete event buffered, losing an invalidation and its advisory cursor until another reconnect. The old 1 Mi limit also applied to the entire incoming chunk before parsing, rejecting coalesced valid small frames. Aborting inside the first callback did not stop later events already buffered in that chunk.

`streamEvents` now uses a connection-local incremental parser. It recognizes LF, CRLF (including split pairs), and lone CR; retains UTF-8 streaming decoding; dispatches each complete frame independently; ignores incomplete EOF data; and checks session/abort ownership before every frame callback. Completed IDs survive reconnect, empty IDs reset the cursor, and NULL-containing IDs are ignored. Empty data fields dispatch; field parsing removes only one optional ASCII space, preserving other whitespace. Event/data fields reset between frames.

The bound is **1,048,576 decoded UTF-16 code units per frame, counting each logical line ending once**. It is not a byte-size promise. Oversized comments, unknown fields, partial lines, and multiline frames still fail into the existing bounded reconnect path. Many valid frames in a larger network chunk are allowed. `retry` remains inert; this patch does not change authentication, the existing reconnect backoff, event authorization, tenant filtering, wire tagging, or the mandatory synthetic resync on connection.

Reference: [WHATWG event-stream parsing and interpretation](https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream). This custom authenticated transport does not claim to implement the entire native EventSource API.

## Verification on the exact source tree

Dependencies installed from the unchanged `web/package-lock.json` with `npm ci --ignore-scripts --no-audit --no-fund`, using a writable local cache. Node `v24.19.0`, Vitest `4.1.11`; the installed Next.js client/server guide was read before edits.

Commands below run in `web/`:

| Check | Result |
|---|---|
| New `base-stream.test.ts` against original production source | 14 cases: 6 pass, 8 expected regression failures |
| `npm test -- --run lib/api/base-stream.test.ts lib/api/event-stream.test.ts lib/api/base.test.ts lib/api/company-events.test.ts lib/session-safety.test.ts` | 52 pass, 0 fail/skip |
| Independent frozen actual `streamEvents`/session adversarial suite | Original: 5 pass/10 fail; candidate: 15 pass/0 fail |
| Independent actual Vitest run, adding `features/company/company-event-consumer.test.tsx` | 66 pass, 0 fail/skip |
| `npm exec -- tsc --noEmit` and independent `--incremental false` | Pass |
| Changed-four-file ESLint, independent `--max-warnings 0` | Pass, zero errors/warnings |
| `npm run lint` | Exit 0; 4 warnings in untouched files, no errors |
| `npm run build` | Pass; 29/29 static pages generated |
| `npm test` | Exit 1; 46 files passed, 2 failed; 620 tests passed, 1 failed, 3 skipped |

The full-suite failures were independently reproduced on an isolated archive of the exact baseline, running only the two affected files: `components/company/r5-protocol.test.tsx` invokes `go test` but the exact Go 1.25.7 toolchain is unavailable; `r5-external-batch-probe.test.tsx` requires an explicit private PostgreSQL fixture. The three skipped cases are from the failed protocol-suite setup and do **not** count as passes. No test or fixture gate was disabled to make the full suite green.

The independent contract was frozen before candidate exposure (SHA-256 `967025f38f11508f4f015ed5bcfee94f1e404936d6c94075e2c59b20867c9349`); its final pre-exposure suite SHA-256 is `383b4ccd78d4c67c011652e482c85eef91f0918eba8bb7b021b5e6a969165297`. It covers split CRLF, byte-split UTF-8, coalesced frames, per-frame overflow before EOF, field/ID grammar, partial EOF, resync/cursor reconnect, and abort/session boundaries.

## Preserved boundaries and remaining acceptance

No Go source, `go.mod`/`go.sum`, npm manifest/lock, fork, migration, R5 registry, receipt, preparation, or historical evidence changed. The Go pin remains exactly 1.25.7. No live account, mail, SMTP delivery, production database, credential, deployment, or merge was used.

Real PostgreSQL commit-order inversion, expired cursors, multiple backend instances, sustained browser sessions, and complete browser/backend workflow acceptance remain unrun in this slice. These remain necessary for R5-P7-090 and G7; passing synthetic ReadableStream/React tests and a production build does not close them.

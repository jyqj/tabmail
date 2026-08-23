# Vendored API documentation assets

Self-hosted so `/docs` and `/redoc` never load executable script from a
third-party CDN. Embedded into the binary via `go:embed` and served under
`/docs-assets/`.

| File | Version | Source | SHA-256 |
|---|---|---|---|
| `swagger-ui.css` | swagger-ui-dist 5.29.5 | unpkg.com/swagger-ui-dist@5.29.5/swagger-ui.css | `bc5e8d5c013477cf1f35e2fb8ba1dff66be0f72f24e669a509635657145e1acb` |
| `swagger-ui-bundle.js` | swagger-ui-dist 5.29.5 (Apache-2.0) | unpkg.com/swagger-ui-dist@5.29.5/swagger-ui-bundle.js | `a646692ba5c95a74f99bb2c15ac879dec9a0001a72aed133ad65068da9e90c97` |
| `redoc.standalone.js` | redoc 2.5.0 (MIT) | cdn.redoc.ly/redoc/v2.5.0/bundles/redoc.standalone.js | `0ec05be285ac885a330289b02f470e1bdbd2b6b3223a9fa213f24bf805a851d1` |

To upgrade: download the new pinned version, verify the checksum against the
npm tarball, and update this table.

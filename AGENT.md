# Agent guidance for Weer

## Project and layout

Weer is a standalone, self-hosted U.S. NEXRAD Level II radar viewer. A Go service downloads volumes, persists metadata in SQLite, invokes a bounded Python/Py-ART subprocess, and serves reflectivity PNGs to a React/OpenLayers frontend.

- `cmd/weer/`: CLI commands (`serve`, `ingest`, `version`).
- `internal/config/`: configuration loading and validation.
- `internal/radar/`: public archive discovery and downloads.
- `internal/ingest/`: job scheduling, retries, processing, and retention.
- `internal/store/`: SQLite persistence and scan lifecycle.
- `internal/processor/`: subprocess execution and output validation.
- `internal/httpapi/`: HTTP API and frontend serving.
- `processor/weer_processor/`: radar decoding, sweep selection, rasterization, and manifests; tests live in `processor/tests/`.
- `web/src/`: React UI, OpenLayers map, and playback logic/tests.
- `configs/`: local and Docker configuration examples.
- `docs/`: architecture, API, processor contract/schema, sources, and validation records.
- `scripts/smoke.sh`: opt-in integration check against real archived observations.

Read `README.md` and the relevant documents before changing behavior. Treat `docs/processor-contract.md` and `docs/manifest.schema.json` as the cross-language processing contract; keep them aligned with implementation changes.

## Setup and commands

Run commands from the repository root unless specified otherwise. The documented toolchain is Go 1.26+, Node.js 22.12+ (24 recommended), pnpm 11.25, Python 3.12, and uv.

```sh
make processor                         # create .venv and install pinned processor dependencies
make build                             # install frontend dependencies, build frontend and Go binary
./weer serve --config configs/weer.yaml
```

For frontend development, run `cd web && pnpm dev` alongside the Go service on port 8080. Configuration paths resolve relative to the working directory.

## Validation

`make test` runs the offline Go, Python, and frontend suites; install dependencies first. For targeted changes, use the relevant checks:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/weer
.venv/bin/python -m unittest discover -s processor/tests -v
cd web && pnpm test && pnpm build
```

CI runs Go race tests, vet, and build; Python unit tests; and frontend tests/build. Format changed Go code with `gofmt`. Match existing Python and TypeScript conventions. Add focused regression coverage for behavioral fixes, especially lifecycle transitions, processor validation, geometry, and playback state.

After building and installing the processor, `make smoke` downloads two checksum-verified public volumes and checks decoding, API/PNG output, duplicate import reuse, and restart persistence. It requires network access and is separate from the offline suite. Do not claim it passed unless it was run. Report checks performed and any checks blocked by unavailable dependencies or network access.

## Behavior to preserve

- Keep one service/import process per data directory, with the database inside that directory. Preserve the advisory lock and bounded worker/subprocess execution.
- HTTP reads serve persisted artifacts and must not launch processing. Validate processor output before publishing frames; preserve temporary-directory staging, atomic publication, and restart recovery.
- Preserve source and render identity, duplicate reuse, bounded retries, and retention semantics. Imported historical volumes retain their real observation times even when retention starts at import.
- Keep processor stdout machine-readable; send diagnostics and library banners to stderr. Update version/render identity when changes would invalidate cached output.
- Distinguish missing reflectivity, valid all-masked reflectivity, stale feeds, and processing errors. Preserve transparent masks and documented sweep selection, geometry, and color-scale behavior.
- Rendered PNGs are visualization products, not numerical measurement grids. Do not reconstruct measurements from colors or describe selected-elevation output as a surface estimate or composite.
- Preserve historical selection when new frames arrive; only live mode follows the newest frame. Keep one OpenLayers map instance and bounded image caching, and guard against stale asynchronous responses.
- Use configured station coordinates and documented sources. Public archive access requires no AWS credentials. Keep the optional basemap's attribution and the independent-NOAA-product notice accurate.

## Change hygiene

Keep changes scoped to the requested task and preserve unrelated local work. Update relevant documentation and configuration examples when interfaces or settings change. Keep dependency declarations and lockfiles consistent.

Do not commit runtime data, downloaded radar archives, credentials, `.venv/`, `web/node_modules/`, `web/dist/`, or the generated `weer` binary. Use temporary data directories for integration work rather than an existing user's radar database.

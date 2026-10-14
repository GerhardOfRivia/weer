# Read-only HTTP API v1

All dates are UTC RFC3339. API errors use `{"error":{"code":"…","message":"…"}}` with 400 for invalid input, 404 for missing/disabled resources, and 500/503 for service failures. There is no upload, arbitrary-file, remote-URL fetch, or mutation endpoint.

| GET | Result |
| --- | --- |
| `/healthz` | Database health; 200 or 503 |
| `/api/v1/status` | Per-station last successful source check, source error, latest discovered nominal time, latest rendered decoded volume time, pending count and recent job errors; stale threshold, retention, basemap config |
| `/api/v1/stations` | Configured station catalog |
| `/api/v1/stations/{id}/scans` | Latest retained scan metadata, returned chronologically with ID tie-breaker |
| `/api/v1/scans/{id}` | One scan, including state, retry details and immutable frame ID |
| `/api/v1/frames/{id}/manifest` | Validated processor manifest |
| `/api/v1/frames/{id}/image.png` | Georeferenced transparent PNG |

Scan queries accept `from`, `to`, and `limit` (1–1000, default 500). Without date filters they return retained scans, including old local imports; with filters the interval must be at most 30 days. A missing `to` defaults to now; a missing `from` defaults to now minus retention. Most recent `limit` scans are selected, then returned in stable ascending chronological order. Pending scan time is explicitly nominal; only successful decoding establishes authoritative observation time.

Frame URLs are content-derived and immutable, with one-year caching and ETags/304. Retention can remove uncached artifacts; an old URL can then return 404. Metadata/status responses are not cached. No local file locations are included in resource metadata. Full error details are retained in SQLite and structured logs; browser job messages omit local filesystem paths. Detailed processing diagnostics are intended for the local operator; do not expose this unauthenticated API to untrusted networks.

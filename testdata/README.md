# Test data policy

Default Go tests generate temporary SQLite databases and mock S3 XML/HTTP responses. Processor tests construct synthetic Py-ART radars entirely in memory. Frontend tests use timestamp-independent scan identities. These fixtures do not claim to validate actual NEXRAD decoding.

The opt-in `scripts/smoke.sh` downloads exactly the two completed archives documented in `docs/sources.md`, verifies their SHA-256 checksums, and exercises real decoding and persistent frame serving. Large radar files, generated radar PNG caches, virtual environments, and dependency directories are intentionally excluded from Git.

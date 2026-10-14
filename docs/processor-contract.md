# Processor contract v1

```sh
python -m weer_processor --input VOLUME --output DIRECTORY \
  --config '{"size":1024,"coverage_km":230,"station":"KFTG","latitude":39.786639,"longitude":-104.545806}'
```

Exit 0: stdout contains `{"ok":true,"manifest":"manifest.json"}`. Exit 2: stdout contains `{"ok":false,"error":{"code":"…","message":"…"}}`. Imports, library banners, and diagnostics go to stderr. Go bounds captured output and enforces its subprocess timeout. `image.png` and `manifest.json` must both exist and validate before publication. The machine-readable schema is [manifest.schema.json](manifest.schema.json).

The processor inspects NEXRAD moment metadata with the pinned Py-ART reader's `NEXRADLevel2File.scan_info()` and includes only original scans containing `REF`. This distinguishes a genuinely all-masked REF moment from velocity-only scans with padded reflectivity masks. It reads full volume metadata and separately reads qualifying scans. It chooses the lowest fixed elevation, treating angles within 0.05° as repeated nominal elevations; the latest qualifying sweep end wins. The manifest records the original volume sweep index, actual fixed angle, all qualifying sweeps and their intervals. All-masked REF is successful transparent output. Missing REF is a structured processing error.

## Geometry

Coverage is fixed per configured station, size, and ground-distance radius. A WGS84 geodesic circle sampled every 0.25° is transformed to EPSG:3857 to produce `[minX,minY,maxX,maxY]`. Pixel centers are north-up (row zero north). Mercator coordinates are never treated as exact ground distances. A circle crossing the antimeridian or reaching 85° latitude is rejected; no wrapped or clipped output is silently substituted.

For each target pixel, inverse Mercator gives geographic coordinates. Py-ART's spherical azimuthal-equidistant transform gives local horizontal distance/bearing about the decoded radar site. `antenna_to_cartesian` maps each ray's measured elevation and range to ground-distance gate centers using Py-ART's effective Earth model. Gate supports use midpoint range edges. The nearest azimuth ray is used only within 0.6 times the median positive angular spacing, capped at 0.75°. No averaging or distant-neighbor interpolation is performed. Missing rays, masked gates, invalid values, range gaps beyond the sampled gates, and pixels outside coverage stay transparent. This is a sampled elevation sweep, not a constant-altitude grid or composite.

The fixed color scale has lower thresholds −30, 0, 5, 10, …, 70 dBZ, with explicit RGB colors in every manifest. Values below/above the scale clamp to its end colors; masks remain transparent. Legend, timestamp, basemap and labels are separate UI elements. Numerical measurements remain in the original archive.

Configuration hashes use SHA-256 of sorted, compact JSON. Source hashes are SHA-256 of the original file bytes. Times use UTC RFC3339, with volume and sweep intervals kept separate. Output includes station coordinates/altitude, available fields, reflectivity sweeps, validity flag, projection, dimensions, orientation, scale, processor version, configuration hash, processed time, and warnings.

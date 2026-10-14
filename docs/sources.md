# Data and API sources

Verified against primary sources during implementation:

- [AWS NEXRAD registry](https://registry.opendata.aws/noaa-nexrad/): completed Level II archive `unidata-nexrad-level2`, `us-east-1`, unsigned access; the older NOAA bucket is deprecated. Weer never uses the chunks bucket.
- [Archive documentation](https://github.com/awslabs/open-data-docs/tree/main/docs/noaa/noaa-nexrad): station/date layout.
- [NOAA NCEI KFTG record](https://www.ncdc.noaa.gov/cdo-web/datasets/NEXRAD2/stations/NEXRAD%3AKFTG/detail): DENVER FRONT RANGE AIRPORT, CO US; latitude 39.786639°, longitude −104.545806°. The decoder supplies altitude from each file; no altitude is guessed.
- [Py-ART archive reader](https://arm-doe.github.io/pyart/API/generated/pyart.io.read_nexrad_archive.html), [antenna geometry](https://arm-doe.github.io/pyart/API/generated/pyart.core.antenna_to_cartesian.html), and [NEXRAD example](https://arm-doe.github.io/pyart/examples/io/plot_nexrad_data_aws.html).
- [OpenStreetMap tile usage policy](https://operations.osmfoundation.org/policies/tiles/): bundled background uses the standard HTTPS tile endpoint, browser caching, and visible linked contributor attribution; no tile prefetch or offline download.
- [OpenLayers ImageStatic](https://openlayers.org/en/latest/apidoc/module-ol_source_ImageStatic-Static.html): projection and extent describe the actual image footprint.

A real unsigned ListObjectsV2 request for `2024/05/20/KFTG/`, with `max-keys=3`, returned completed objects ending `_V06`, a continuation token, sizes, and ETags. It also returned July 2025 LastModified values for May 2024 observations. LastModified is deliberately unused for observation time. Accepted filenames are station + YYYYMMDD_HHMMSS, optional `_V06` through `_V09`, optional `.gz`; sidecars such as `_MDM`, empty objects, mismatched station/date prefixes, and other objects are excluded before downloading. Successful listing alone does not prove decodability.

## Real smoke fixtures (not committed)

Bucket: `unidata-nexrad-level2`. Source URL is `https://unidata-nexrad-level2.s3.amazonaws.com/` followed by the key.

| Key | SHA-256 |
| --- | --- |
| `2024/05/20/KFTG/KFTG20240520_000059_V06` | `a489988121ecab65f4dbe09e13f63cd563967294dac981a9c34a0dfd02584c80` |

The first file is 7,391,187 bytes. Real decoding selected original sweep 7 at 0.4833984375°, with interval 2024-05-20T00:03:22.853Z to 00:03:46.588Z; volume start is 00:00:59.979Z. These are decoded times, not filename or S3 timestamps.

Second replay fixture: `2024/05/20/KFTG/KFTG20240520_000528_V06`, SHA-256 `dcc76035ffe66a24642c88800c7275c7a2b84c9dbfd8a44c1c29cf96b1124f71` (7,177,304 bytes). Both checksums are enforced by `scripts/smoke.sh`.

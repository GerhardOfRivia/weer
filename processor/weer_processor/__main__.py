"""Selected polar sweep, sampled at pixel centers; no spatial interpolation."""

import argparse
import contextlib
import hashlib
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

VERSION = "0.1.0"
SCALE = {
    "units": "dBZ",
    "thresholds": [-30, 0, 5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70],
    "colors": [
        "#64748b",
        "#04e9e7",
        "#019ff4",
        "#0300f4",
        "#02fd02",
        "#01c501",
        "#008e00",
        "#fdf802",
        "#e5bc00",
        "#fd9500",
        "#fd0000",
        "#d40000",
        "#bc0000",
        "#f800fd",
        "#9854c6",
        "#ffffff",
    ],
}


def select_sweep(radar):
    import numpy as np

    if "reflectivity" not in radar.fields:
        raise ValueError("missing_reflectivity: volume has no reflectivity field")
    # Reader excludes velocity-only scans (scans with no REF moment).
    sweeps = [
        {"index": i, "elevation": float(radar.fixed_angle["data"][i])}
        for i in range(radar.nsweeps)
    ]
    low = min(s["elevation"] for s in sweeps)
    candidates = [s for s in sweeps if abs(s["elevation"] - low) <= 0.05]
    selected = max(
        candidates, key=lambda s: float(radar.time["data"][radar.get_end(s["index"])])
    )
    return selected, sweeps


def rasterize(radar, sweep, cfg):
    import numpy as np
    import pyart
    from pyproj import Transformer, Geod

    lat, lon = float(radar.latitude["data"][0]), float(radar.longitude["data"][0])
    radius = cfg["coverage_km"] * 1000
    geod = Geod(ellps="WGS84")
    az = np.arange(0, 360, 0.25)
    lons, lats, _ = geod.fwd(
        np.full(az.shape, cfg["longitude"]),
        np.full(az.shape, cfg["latitude"]),
        az,
        np.full(az.shape, radius),
    )
    if np.max(np.abs(lats)) >= 85 or np.ptp(lons) > 180:
        raise ValueError(
            "unsupported_coverage: coverage crosses antimeridian or Mercator polar limit"
        )
    project = Transformer.from_crs(4326, 3857, always_xy=True)
    xx, yy = project.transform(lons, lats)
    extent = [float(min(xx)), float(min(yy)), float(max(xx)), float(max(yy))]
    size = cfg["size"]
    x = extent[0] + (np.arange(size) + 0.5) * (extent[2] - extent[0]) / size
    y = extent[3] - (np.arange(size) + 0.5) * (extent[3] - extent[1]) / size
    gx, gy = np.meshgrid(x, y)
    plon, plat = Transformer.from_crs(3857, 4326, always_xy=True).transform(gx, gy)
    localx, localy = pyart.core.geographic_to_cartesian_aeqd(plon, plat, lon, lat)
    distance = np.hypot(localx, localy)
    bearing = np.rad2deg(np.arctan2(localx, localy)) % 360
    sl = radar.get_slice(sweep)
    rays = np.asarray(radar.azimuth["data"][sl]) % 360
    order = np.argsort(rays)
    sorted_az = rays[order]
    # Circular nearest ray; angular support is capped at 0.75 degrees.
    right = np.searchsorted(sorted_az, bearing) % len(rays)
    left = (right - 1) % len(rays)
    delta = lambda a: np.abs((bearing - a + 180) % 360 - 180)
    choose = np.where(delta(sorted_az[left]) <= delta(sorted_az[right]), left, right)
    rayidx = order[choose]
    angular = delta(rays[rayidx])
    spacing = np.diff(np.r_[sorted_az, sorted_az[0] + 360])
    halfwidth = min(0.75, float(np.median(spacing[spacing > 0])) * 0.6)
    ranges = np.asarray(radar.range["data"])
    elevations = np.asarray(radar.elevation["data"][sl])
    data = radar.get_field(sweep, "reflectivity")
    result = np.ma.masked_all((size, size), dtype=float)
    # Each ray uses its measured elevation and Py-ART's 4/3 Earth geometry.
    for ri in np.unique(rayidx):
        px, py, _ = pyart.core.antenna_to_cartesian(
            ranges / 1000, np.zeros(len(ranges)), np.full(len(ranges), elevations[ri])
        )
        ground = np.hypot(px, py)
        edges = np.r_[
            max(0, ground[0] - (ground[1] - ground[0]) / 2),
            (ground[:-1] + ground[1:]) / 2,
            ground[-1] + (ground[-1] - ground[-2]) / 2,
        ]
        pixels = (rayidx == ri) & (angular <= halfwidth) & (distance <= radius)
        gates = np.searchsorted(edges, distance[pixels], side="right") - 1
        valid = (gates >= 0) & (gates < len(ranges))
        values = np.ma.masked_all(gates.shape)
        values[valid] = data[ri, gates[valid]]
        result[pixels] = values
    rgba = np.zeros((size, size, 4), dtype=np.uint8)
    valid = ~np.ma.getmaskarray(result) & np.isfinite(result.filled(np.nan))
    indices = np.clip(
        np.searchsorted(SCALE["thresholds"], result.filled(-999), side="right") - 1,
        0,
        len(SCALE["colors"]) - 1,
    )
    colors = np.array(
        [[int(c[i : i + 2], 16) for i in (1, 3, 5)] + [255] for c in SCALE["colors"]],
        dtype=np.uint8,
    )
    rgba[valid] = colors[indices[valid]]
    return rgba, extent


def process(input_path, output, cfg):
    import numpy as np
    import pyart
    from PIL import Image
    from netCDF4 import num2date

    # NEXRAD reader normally retains velocity-only sweeps with masked REF.
    # Inspect original moment dictionaries to select only scans containing REF.
    from pyart.io.nexrad_level2 import NEXRADLevel2File

    archive = NEXRADLevel2File(pyart.io.common.prepare_for_read(str(input_path)))
    scans = [
        i for i, info in enumerate(archive.scan_info()) if "REF" in info["moments"]
    ]
    archive.close()
    if not scans:
        raise ValueError("missing_reflectivity: no REF moment in volume")
    full = pyart.io.read_nexrad_archive(str(input_path))
    radar = pyart.io.read_nexrad_archive(str(input_path), scans=scans)
    selected, available = select_sweep(radar)
    idx = selected["index"]

    def stamp(r, n):
        d = num2date(
            float(r.time["data"][n]), r.time["units"], only_use_cftime_datetimes=False
        )
        return d.replace(tzinfo=timezone.utc).isoformat().replace("+00:00", "Z")

    def interval(r, i):
        return {"start": stamp(r, r.get_start(i)), "end": stamp(r, r.get_end(i))}

    for s in available:
        s.update(interval(radar, s["index"]))
        s["index"] = scans[s["index"]]
    rgba, extent = rasterize(radar, idx, cfg)
    station = str(radar.metadata.get("instrument_name", "")).strip()
    if station != cfg["station"]:
        raise ValueError(
            f"station_mismatch: decoded {station!r}, expected {cfg['station']}"
        )
    with open(input_path, "rb") as source:
        fingerprint = hashlib.file_digest(source, "sha256").hexdigest()
    config_hash = hashlib.sha256(
        json.dumps(cfg, sort_keys=True, separators=(",", ":")).encode()
    ).hexdigest()
    manifest = {
        "schema_version": 1,
        "source": {"identifier": input_path.name, "sha256": fingerprint},
        "station": {
            "id": station,
            "latitude": float(radar.latitude["data"][0]),
            "longitude": float(radar.longitude["data"][0]),
            "altitude": float(radar.altitude["data"][0]),
        },
        "volume_time": {"start": stamp(full, 0), "end": stamp(full, full.nrays - 1)},
        "sweep_time": interval(radar, idx),
        "field": "reflectivity",
        "units": "dBZ",
        "sweep_index": scans[idx],
        "elevation": selected["elevation"],
        "available_fields": list(full.fields),
        "reflectivity_sweeps": available,
        "has_valid_measurements": bool(
            np.ma.count(radar.get_field(idx, "reflectivity"))
        ),
        "projection": "EPSG:3857",
        "extent": extent,
        "width": cfg["size"],
        "height": cfg["size"],
        "pixel_orientation": "row_0_north",
        "image": "image.png",
        "color_scale": SCALE,
        "processed_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "processor_version": VERSION,
        "render_config_sha256": config_hash,
        "warnings": []
        if np.ma.count(radar.get_field(idx, "reflectivity"))
        else ["Selected reflectivity sweep contains no valid measurements."],
    }
    output.mkdir(parents=True, exist_ok=True)
    Image.fromarray(rgba).save(output / "image.png")
    (output / "manifest.json").write_text(json.dumps(manifest, allow_nan=False))
    return manifest


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--config", required=True, help="JSON render configuration")
    args = parser.parse_args()
    try:
        cfg = json.loads(args.config)
        if (
            set(cfg) != {"size", "coverage_km", "station", "latitude", "longitude"}
            or not -80 <= cfg["latitude"] <= 80
            or not -180 <= cfg["longitude"] <= 180
            or not isinstance(cfg["station"], str)
            or len(cfg["station"]) != 4
            or not isinstance(cfg["size"], int)
            or not 128 <= cfg["size"] <= 2048
            or not 10 <= cfg["coverage_km"] <= 460
        ):
            raise ValueError("invalid_configuration")
        with contextlib.redirect_stdout(sys.stderr):
            manifest = process(args.input, args.output, cfg)
        print(json.dumps({"ok": True, "manifest": "manifest.json"}))
    except Exception as exc:
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": {"code": type(exc).__name__, "message": str(exc)},
                }
            )
        )
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())

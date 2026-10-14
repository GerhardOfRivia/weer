import unittest
import contextlib
import sys

with contextlib.redirect_stdout(sys.stderr):
    import numpy as np
    import pyart
from weer_processor.__main__ import select_sweep, rasterize, SCALE


class ProcessorTests(unittest.TestCase):
    def radar(self, sweeps=1):
        r = pyart.testing.make_empty_ppi_radar(100, 360, sweeps)
        r.latitude["data"][:] = 39.786639
        r.longitude["data"][:] = -104.545806
        r.range["data"] = np.arange(100) * 1000.0 + 500
        r.azimuth["data"] = np.tile(np.arange(360.0), sweeps)
        r.elevation["data"][:] = 0.5
        r.fixed_angle["data"][:] = 0.5
        r.time["data"] = np.arange(360 * sweeps, dtype=float)
        r.add_field(
            "reflectivity",
            {"data": np.ma.array(np.full((360 * sweeps, 100), 30.0)), "units": "dBZ"},
        )
        return r

    def cfg(self):
        return dict(
            size=128,
            coverage_km=100,
            station="KFTG",
            latitude=39.786639,
            longitude=-104.545806,
        )

    def test_lowest_latest(self):
        r = self.radar(3)
        r.fixed_angle["data"][:] = [1, 0.5, 0.5]
        selected, available = select_sweep(r)
        self.assertEqual(selected["index"], 2)
        self.assertEqual(len(available), 3)

    def test_missing_field(self):
        r = self.radar()
        r.fields = {}
        with self.assertRaisesRegex(ValueError, "missing_reflectivity"):
            select_sweep(r)

    def test_all_masked(self):
        r = self.radar()
        r.fields["reflectivity"]["data"] = np.ma.masked_all((360, 100))
        self.assertEqual(select_sweep(r)[0]["index"], 0)
        img, _ = rasterize(r, 0, self.cfg())
        self.assertEqual(img[:, :, 3].sum(), 0)

    def test_orientation_extent_scale_and_sectors(self):
        r = self.radar()
        data = r.fields["reflectivity"]["data"]
        for ray in range(360):
            data[ray, :] = [10, 20, 40, 60][int((ray + 45) % 360 // 90)]
        img, extent = rasterize(r, 0, self.cfg())
        self.assertLess(extent[0], extent[2])
        self.assertLess(extent[1], extent[3])

        def rgb(value):
            c = SCALE["colors"][SCALE["thresholds"].index(value)]
            return [int(c[i : i + 2], 16) for i in (1, 3, 5)]

        for row, col, value in [(32, 64, 10), (64, 96, 20), (96, 64, 40), (64, 32, 60)]:
            self.assertEqual(img[row, col, :3].tolist(), rgb(value))
            self.assertEqual(img[row, col, 3], 255)
        # Missing northeastern sector stays transparent, even with valid distant neighbors.
        data.mask = np.zeros(data.shape, dtype=bool)
        data.mask[20:70, :] = True
        img2, extent2 = rasterize(r, 0, self.cfg())
        self.assertEqual(img2[32, 96, 3], 0)
        self.assertEqual(extent, extent2)
        self.assertEqual(img2[0, 0, 3], 0)

    def test_missing_rays_not_bridged(self):
        r = self.radar()
        r.azimuth["data"][20:70] = 180
        img, _ = rasterize(r, 0, self.cfg())
        self.assertEqual(img[32, 96, 3], 0)

    def test_polar_coverage_rejected(self):
        cfg = self.cfg()
        cfg["latitude"] = 85
        with self.assertRaisesRegex(ValueError, "unsupported_coverage"):
            rasterize(self.radar(), 0, cfg)


if __name__ == "__main__":
    unittest.main()

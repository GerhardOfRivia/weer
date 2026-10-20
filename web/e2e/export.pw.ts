import { test, expect } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  const images = await page.evaluate(() =>
    ["#ff0000", "#00ff00"].map((color) => {
      const c = document.createElement("canvas");
      c.width = c.height = 128;
      const ctx = c.getContext("2d")!;
      ctx.fillStyle = color;
      ctx.fillRect(32, 32, 64, 64);
      return c.toDataURL().split(",")[1];
    }),
  );
  await page.route("https://tile.openstreetmap.org/**", (route) =>
    route.fulfill({
      contentType: "image/svg+xml",
      headers: { "access-control-allow-origin": "*" },
      body: '<svg xmlns="http://www.w3.org/2000/svg" width="256" height="256"><path fill="#dddddd" d="M0 0h256v256H0z"/><path stroke="#555555" stroke-width="3" d="M0 0L256 256M0 128H256"/><text x="20" y="60">Test city</text></svg>',
    }),
  );
  await page.route("**/api/v1/**", (route) => {
    const path = new URL(route.request().url()).pathname;
    let json: unknown;
    if (path.endsWith("/stations"))
      json = [
        {
          id: "KFTG",
          name: "Test station",
          longitude: -104.545806,
          latitude: 39.786639,
        },
      ];
    else if (path.endsWith("/status"))
      json = {
        stations: [],
        stale_after_seconds: 900,
        basemap_url: "https://tile.openstreetmap.org/{z}/{x}/{y}.png",
        basemap_attribution: "© OpenStreetMap contributors",
      };
    else if (path.endsWith("/scans"))
      json = [2, 1].map((n) => ({
        id: `s${n}`,
        frame_id: `f${n}`,
        station: "KFTG",
        state: "ready",
        observation_time: `2026-10-20T14:${n === 1 ? "30" : "35"}:00Z`,
      }));
    else if (path.endsWith("/manifest"))
      json = {
        station: { id: "KFTG" },
        elevation: 0.5,
        sweep_time: {
          start: `2026-10-20T14:${path.includes("f1") ? "30" : "35"}:00Z`,
        },
        projection: "EPSG:3857",
        extent: [-11800000, 4600000, -11400000, 5000000],
        has_valid_measurements: true,
        color_scale: { thresholds: [0, 10], colors: ["#ff0000", "#00ff00"] },
      };
    else if (path.endsWith(".png"))
      return route.fulfill({
        contentType: "image/png",
        body: Buffer.from(images[path.includes("f1") ? 0 : 1], "base64"),
      });
    else return route.abort();
    return route.fulfill({ json });
  });
});

test("exports actual background and radar after pan/zoom without stopping playback", async ({
  page,
}, testInfo) => {
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeEnabled();
  await expect(page.locator(".observation time")).toBeVisible();
  await page.locator(".ol-zoom-in").click();
  await page.mouse.move(850, 400);
  await page.mouse.down();
  await page.mouse.move(930, 440, { steps: 10 });
  await page.mouse.up();
  await page.waitForTimeout(600);
  await page.getByRole("button", { name: "Play", exact: true }).click();
  const transform = await page
    .locator(".basemap-layer canvas")
    .getAttribute("style");
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export GIF", exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe("weer_KFTG_20261020_143500.gif");
  await file.saveAs(testInfo.outputPath("radar.gif"));
  await expect(
    page.getByRole("button", { name: "Pause", exact: true }),
  ).toBeVisible();
  expect(
    await page.locator(".basemap-layer canvas").getAttribute("style"),
  ).toBe(transform);
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeEnabled();
  const bytes = await (await file.createReadStream())!.toArray();
  const gif = Buffer.concat(bytes);
  expect(gif.subarray(0, 6).toString()).toBe("GIF89a");
  expect(gif.readUInt16LE(6)).toBe(1024);
  expect(gif.at(-1)).toBe(0x3b);
  const frames = await page.evaluate(async (data) => {
    const decoder = new (window as any).ImageDecoder({
      type: "image/gif",
      data: Uint8Array.from(atob(data), (c) => c.charCodeAt(0)),
    });
    await decoder.tracks.ready;
    const results = [];
    for (let i = 0; i < decoder.tracks.selectedTrack.frameCount; i++) {
      const { image } = await decoder.decode({ frameIndex: i });
      const c = document.createElement("canvas");
      c.width = image.displayWidth;
      c.height = image.displayHeight;
      const ctx = c.getContext("2d")!;
      ctx.drawImage(image, 0, 0);
      image.close();
      const pixels = ctx.getImageData(0, 40, c.width, c.height - 80).data;
      let red = 0,
        green = 0,
        gray = 0;
      for (let p = 0; p < pixels.length; p += 4) {
        if (pixels[p] > pixels[p + 1] + 50) red++;
        if (pixels[p + 1] > pixels[p] + 50) green++;
        if (pixels[p] === pixels[p + 1] && pixels[p] > 50) gray++;
      }
      results.push({ red, green, gray });
    }
    decoder.close();
    return results;
  }, gif.toString("base64"));
  expect(frames).toHaveLength(2);
  expect(frames[0].red).toBeGreaterThan(100);
  expect(frames[0].green).toBe(0);
  expect(frames[1].green).toBe(frames[0].red);
  expect(frames[1].red).toBe(0);
  for (const frame of frames) expect(frame.gray).toBeGreaterThan(10000);
});

test("cancels a pending export and permits retry", async ({ page }) => {
  await page.route("https://tile.openstreetmap.org/**", async (route) => {
    await new Promise((resolve) => setTimeout(resolve, 1500));
    await route.fallback();
  });
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeEnabled();
  await page.getByRole("button", { name: "Export GIF", exact: true }).click();
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeDisabled();
  await page.getByRole("button", { name: "Cancel export" }).click();
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeEnabled();
  await expect(page.getByRole("button", { name: "Cancel export" })).toHaveCount(
    0,
  );
});

test("reports tile errors without downloading an incomplete map", async ({
  page,
}) => {
  await page.route("https://tile.openstreetmap.org/**", (route) =>
    route.abort(),
  );
  await page.goto("/");
  await expect(
    page.getByRole("button", { name: "Export GIF", exact: true }),
  ).toBeEnabled();
  await page.waitForTimeout(500);
  await page.getByRole("button", { name: "Export GIF", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("map tiles failed");
});

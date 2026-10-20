import type Map from "ol/Map";
import type RenderEvent from "ol/render/Event";
import type { Scan } from "./playback";
import type { Manifest, RadarFrame } from "./radar-frame";

export const MAX_FRAMES = 120;
export function exportFrames(scans: Scan[]) {
  const seen = new Set<string>();
  return scans
    .filter((s) => s.state === "ready" && s.frame_id)
    .sort(
      (a, b) => Date.parse(a.observation_time) - Date.parse(b.observation_time),
    )
    .filter((s) => !seen.has(s.frame_id) && !!seen.add(s.frame_id));
}
export function filename(station: string, time: string) {
  return `weer_${station}_${new Date(time).toISOString().replace(/[-:]/g, "").replace("T", "_").slice(0, 15)}.gif`;
}
export function radarTransform(
  extent: number[],
  width: number,
  height: number,
  t: number[],
) {
  const [left, bottom, right, top] = extent;
  const dx = (right - left) / width,
    dy = (bottom - top) / height;
  return [
    t[0] * dx,
    t[1] * dx,
    t[2] * dy,
    t[3] * dy,
    t[0] * left + t[2] * top + t[4],
    t[1] * left + t[3] * top + t[5],
  ];
}
function canvas(width: number, height: number) {
  const c = document.createElement("canvas");
  c.width = width;
  c.height = height;
  return c;
}
function viewKey(map: Map) {
  const v = map.getView();
  return JSON.stringify([
    map.getSize(),
    v.getCenter(),
    v.getResolution(),
    v.getRotation(),
  ]);
}
// Copy only the basemap layer, never the controls or the active radar layer.
export function captureBackground(map: Map, signal: AbortSignal) {
  const initial = viewKey(map);
  return new Promise<{
    background: HTMLCanvasElement;
    transform: number[];
    attribution: string;
    projection: string;
  }>((resolve, reject) => {
    const cleanup = () => {
      clearTimeout(timer);
      map.un("rendercomplete", complete);
      signal.removeEventListener("abort", abort);
    };
    const fail = (error: unknown) => {
      cleanup();
      reject(error);
    };
    const abort = () => fail(new DOMException("Cancelled", "AbortError"));
    const timer = setTimeout(
      () =>
        fail(
          Error(
            "Map tiles did not finish loading. Wait for the map and try again.",
          ),
        ),
      20000,
    );
    const complete = (event: RenderEvent) => {
      let background: HTMLCanvasElement | undefined;
      try {
        signal.throwIfAborted();
        if (initial !== viewKey(map))
          throw Error(
            "The viewport changed while loading tiles. Try Export GIF again.",
          );
        const state = event.frameState!;
        const [w, h] = state.size;
        const height = Math.round((1024 * h) / w);
        if (height < 64 || height > 2048)
          throw Error(
            "Resize the map to a less extreme aspect ratio before exporting.",
          );
        background = canvas(1024, height);
        const ctx = background.getContext("2d")!;
        const css = getComputedStyle(map.getTargetElement()!);
        ctx.fillStyle = css.getPropertyValue("--surface").trim() || "#edf3f6";
        ctx.fillRect(0, 0, 1024, height);
        const layer = map
          .getViewport()!
          .querySelector<HTMLElement>(".basemap-layer");
        const tiles = layer?.querySelector("canvas");
        if (!tiles || !tiles.width)
          throw Error("A loaded basemap is required to export a GIF.");
        const matrix = new DOMMatrix(tiles.style.transform || undefined);
        ctx.scale(1024 / w, height / h);
        ctx.transform(
          matrix.a,
          matrix.b,
          matrix.c,
          matrix.d,
          matrix.e,
          matrix.f,
        );
        ctx.filter = getComputedStyle(layer!).filter;
        ctx.drawImage(tiles, 0, 0);
        // Fail here, before loading radar data, if the provider taints the canvas.
        ctx.getImageData(0, 0, 1, 1);
        const attribution = Array.from(
          map.getViewport()!.querySelectorAll(".ol-attribution li"),
        )
          .map((el) => el.textContent?.trim())
          .filter(Boolean)
          .join(" · ");
        if (!attribution)
          throw Error(
            "Map attribution is unavailable. Wait for the map and try again.",
          );
        const t = state.coordinateToPixelTransform;
        const transform = [
          (t[0] * 1024) / w,
          (t[1] * height) / h,
          (t[2] * 1024) / w,
          (t[3] * height) / h,
          (t[4] * 1024) / w,
          (t[5] * height) / h,
        ];
        cleanup();
        resolve({
          background,
          transform,
          attribution,
          projection: state.viewState.projection.getCode(),
        });
      } catch (error) {
        if (background) background.width = background.height = 0;
        fail(error);
      }
    };
    signal.addEventListener("abort", abort, { once: true });
    map.on("rendercomplete", complete);
    if (signal.aborted) abort();
    else map.render();
  });
}
export function composeFrame(
  background: HTMLCanvasElement,
  output: HTMLCanvasElement,
  frame: RadarFrame,
  transform: number[],
  opacity: number,
  attribution: string,
) {
  const ctx = output.getContext("2d")!;
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.globalAlpha = 1;
  ctx.drawImage(background, 0, 0);
  ctx.save();
  ctx.globalAlpha = opacity;
  ctx.imageSmoothingEnabled = false;
  const t = radarTransform(
    frame.manifest.extent,
    frame.image.naturalWidth,
    frame.image.naturalHeight,
    transform,
  );
  ctx.setTransform(t[0], t[1], t[2], t[3], t[4], t[5]);
  ctx.drawImage(frame.image, 0, 0);
  ctx.restore();
  const time = new Date(frame.manifest.sweep_time.start)
    .toISOString()
    .replace("T", " ")
    .replace(".000Z", " UTC");
  ctx.font = "16px sans-serif";
  const words = attribution.split(/\s+/),
    lines: string[] = [];
  let line = "";
  for (const word of words) {
    if (ctx.measureText(`${line} ${word}`).width > output.width - 24 && line) {
      lines.push(line);
      line = "";
    }
    line += `${line ? " " : ""}${word}`;
  }
  lines.push(line);
  ctx.fillStyle = "rgba(0,0,0,0.8)";
  ctx.fillRect(0, 0, output.width, 32);
  ctx.fillRect(
    0,
    output.height - lines.length * 22 - 8,
    output.width,
    lines.length * 22 + 8,
  );
  ctx.fillStyle = "#fff";
  ctx.fillText(`${frame.manifest.station.id} · ${time}`, 12, 22);
  lines.forEach((text, i) =>
    ctx.fillText(text, 12, output.height - (lines.length - i - 1) * 22 - 10),
  );
  return ctx.getImageData(0, 0, output.width, output.height);
}
export async function exportGIF(options: {
  map: Map;
  scans: Scan[];
  station: string;
  opacity: number;
  signal: AbortSignal;
  metadata: (scan: Scan) => Promise<Manifest>;
  load: (scan: Scan, manifest: Manifest) => Promise<RadarFrame>;
  progress: (message: string) => void;
  tilesFailed: () => boolean;
}) {
  const { signal } = options;
  const scans = exportFrames(options.scans);
  if (scans.length < 2)
    throw Error("At least two distinct radar frames are required.");
  if (scans.length > MAX_FRAMES)
    throw Error(
      `Export supports up to ${MAX_FRAMES} frames; this animation has ${scans.length}.`,
    );
  options.progress("Preparing map…");
  const snapshot = await captureBackground(options.map, signal).catch(
    (error) => {
      if (error instanceof DOMException && error.name === "SecurityError")
        throw Error(
          "The basemap provider does not permit canvas export (CORS). Configure an export-compatible tile provider.",
        );
      throw error;
    },
  );
  let worker: Worker | undefined;
  let output: HTMLCanvasElement | undefined;
  try {
    if (options.tilesFailed())
      throw Error(
        "Some map tiles failed to load. Reload the map and try again.",
      );
    output = canvas(snapshot.background.width, snapshot.background.height);
    worker = new Worker(new URL("./gif.worker.ts", import.meta.url), {
      type: "module",
    });
    const exchange = (message: object, transfer: Transferable[] = []) =>
      new Promise<any>((resolve, reject) => {
        const cleanup = () => {
          clearTimeout(timer);
          signal.removeEventListener("abort", abort);
          worker!.onmessage = null;
          worker!.onerror = null;
        };
        const abort = () => {
          cleanup();
          reject(new DOMException("Cancelled", "AbortError"));
        };
        const timer = setTimeout(() => {
          cleanup();
          reject(Error("GIF encoder timed out."));
        }, 30000);
        signal.addEventListener("abort", abort, { once: true });
        worker!.onerror = () => {
          cleanup();
          reject(Error("GIF encoder failed."));
        };
        worker!.onmessage = ({ data }) => {
          cleanup();
          data.type === "error" ? reject(Error(data.message)) : resolve(data);
        };
        if (signal.aborted) abort();
        else worker!.postMessage(message, transfer);
      });
    // Metadata is small; only one decoded export frame and one RGBA buffer are in flight.
    const ordered: { scan: Scan; time: number; manifest: Manifest }[] = [];
    for (const scan of scans) {
      signal.throwIfAborted();
      options.progress(
        `Reading observation times ${ordered.length + 1} of ${scans.length}…`,
      );
      const manifest = await options.metadata(scan);
      if (manifest.station.id !== options.station)
        throw Error("Station mismatch");
      const time = Date.parse(manifest.sweep_time.start);
      if (!Number.isFinite(time)) throw Error("Invalid observation timestamp.");
      ordered.push({ scan, time, manifest });
    }
    ordered.sort((a, b) => a.time - b.time);
    let latest = "";
    let encoded = 0;
    let previousPixels: Uint8ClampedArray | undefined;
    let previousExtent = "";
    for (let i = 0; i < ordered.length; i++) {
      signal.throwIfAborted();
      options.progress(`Encoding frame ${i + 1} of ${ordered.length}…`);
      const frame = await options.load(ordered[i].scan, ordered[i].manifest);
      if (frame.manifest.projection !== snapshot.projection)
        throw Error("Radar projection does not match the map.");
      // Compare the decoded radar pixels before adding the timestamp or basemap.
      const pixels = canvas(
        frame.image.naturalWidth,
        frame.image.naturalHeight,
      );
      let unchanged = false;
      try {
        const ctx = pixels.getContext("2d")!;
        ctx.drawImage(frame.image, 0, 0);
        const data = ctx.getImageData(0, 0, pixels.width, pixels.height).data;
        const extent = `${pixels.width},${pixels.height}:${frame.manifest.extent.join(",")}`;
        unchanged =
          extent === previousExtent &&
          !!previousPixels &&
          data.length === previousPixels.length &&
          data.every((value, i) => value === previousPixels![i]);
        previousPixels = data;
        previousExtent = extent;
      } finally {
        pixels.width = pixels.height = 0;
      }
      if (unchanged) continue;
      latest = frame.manifest.sweep_time.start;
      const rgba = composeFrame(
        snapshot.background,
        output,
        frame,
        snapshot.transform,
        options.opacity,
        snapshot.attribution,
      );
      await exchange(
        {
          type: "frame",
          buffer: rgba.data.buffer,
          width: output.width,
          height: output.height,
        },
        [rgba.data.buffer],
      );
      encoded++;
    }
    if (encoded < 2)
      throw Error(
        "Radar data is unchanged; at least two distinct images are required for an animation.",
      );
    const result = await exchange({ type: "finish" });
    signal.throwIfAborted();
    const url = URL.createObjectURL(
      new Blob([result.bytes], { type: "image/gif" }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = filename(options.station, latest);
    document.body.appendChild(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (error) {
    if (error instanceof DOMException && error.name === "SecurityError")
      throw Error(
        "The basemap provider does not permit canvas export (CORS). Configure an export-compatible tile provider.",
      );
    throw error;
  } finally {
    worker?.terminate();
    snapshot.background.width = snapshot.background.height = 0;
    if (output) output.width = output.height = 0;
  }
}

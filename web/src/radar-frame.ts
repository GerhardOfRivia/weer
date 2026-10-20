import type { Scan } from "./playback";
export type Manifest = {
  station: { id: string };
  field: string;
  units: string;
  elevation: number;
  sweep_time: { start: string; end: string };
  volume_time: { start: string; end: string };
  has_valid_measurements: boolean;
  projection: string;
  extent: number[];
  color_scale: { thresholds: number[]; colors: string[] };
};
export type RadarFrame = { image: HTMLImageElement; manifest: Manifest };
export async function loadFrame(
  scan: Scan,
  station: string,
  cache: Map<string, RadarFrame>,
  signal: AbortSignal,
  metadata?: Manifest,
) {
  signal.throwIfAborted();
  const old = cache.get(scan.frame_id);
  if (old && old.manifest.station.id === station) return old;
  let manifest = metadata;
  if (!manifest) {
    const response = await fetch(`/api/v1/frames/${scan.frame_id}/manifest`, {
      signal,
    });
    if (!response.ok) throw Error(`Frame unavailable (${response.status})`);
    manifest = (await response.json()) as Manifest;
  }
  if (manifest.station.id !== station) throw Error("Station mismatch");
  const image = new Image();
  const abort = () => {
    image.src = "";
  };
  signal.addEventListener("abort", abort, { once: true });
  try {
    image.src = `/api/v1/frames/${scan.frame_id}/image.png`;
    await image.decode();
    signal.throwIfAborted();
    const entry = { image, manifest };
    cache.set(scan.frame_id, entry);
    while (cache.size > 12) cache.delete(cache.keys().next().value!);
    return entry;
  } finally {
    signal.removeEventListener("abort", abort);
  }
}

import { describe, expect, it, vi } from "vitest";
import {
  exportFrames,
  filename,
  radarTransform,
  composeFrame,
  exportGIF,
} from "./gif-export";
import { createEncoder } from "./gif-encoder";
import type { Scan } from "./playback";
import type { RadarFrame } from "./radar-frame";
const scan = (id: string, time: string, frame = id): Scan => ({
  id,
  frame_id: frame,
  observation_time: time,
  station: "KFTG",
  state: "ready",
  error: "",
});
describe("GIF export", () => {
  it("sorts observations without mutating playback and removes duplicate frame IDs", () => {
    const scans = [
      scan("b", "2026-10-20T14:35:00Z"),
      scan("a", "2026-10-20T14:30:00Z"),
      scan("c", "2026-10-20T14:40:00Z", "b"),
    ];
    expect(exportFrames(scans).map((s) => s.id)).toEqual(["a", "b"]);
    expect(scans.map((s) => s.id)).toEqual(["b", "a", "c"]);
  });
  it("uses UTC in download filenames", () => {
    expect(filename("KFTG", "2026-10-20T14:30:00Z")).toBe(
      "weer_KFTG_20261020_143000.gif",
    );
  });
  it("maps north-up raster corners through a panned, zoomed and rotated viewport", () => {
    for (const t of [
      [2, 0, 0, -2, 80, 300],
      [0, 3, 3, 0, -200, 60],
    ]) {
      const [a, b, c, d, e, f] = radarTransform([10, 20, 30, 40], 100, 100, t);
      expect([e, f]).toEqual([
        t[0] * 10 + t[2] * 40 + t[4],
        t[1] * 10 + t[3] * 40 + t[5],
      ]);
      expect([a * 100 + c * 100 + e, b * 100 + d * 100 + f]).toEqual([
        t[0] * 30 + t[2] * 20 + t[4],
        t[1] * 30 + t[3] * 20 + t[5],
      ]);
    }
  });
  it("composites the same background first, uses radar opacity and actual sweep time on every frame", () => {
    const operations: unknown[] = [];
    const ctx = {
      globalAlpha: 1,
      imageSmoothingEnabled: true,
      setTransform: vi.fn(),
      save: vi.fn(),
      restore() {
        this.globalAlpha = 1;
      },
      drawImage(image: unknown) {
        operations.push([image, this.globalAlpha]);
      },
      fillRect: vi.fn(),
      fillText: vi.fn(),
      measureText: () => ({ width: 20 }),
      getImageData: vi.fn(),
      font: "",
      fillStyle: "",
    };
    const output = {
      width: 1024,
      height: 768,
      getContext: () => ctx,
    } as unknown as HTMLCanvasElement;
    const background = {} as HTMLCanvasElement;
    const frame = {
      image: { naturalWidth: 10, naturalHeight: 10 },
      manifest: {
        extent: [0, 0, 10, 10],
        station: { id: "KFTG" },
        sweep_time: { start: "2026-10-20T14:30:00Z" },
      },
    } as RadarFrame;
    for (let i = 0; i < 2; i++)
      composeFrame(
        background,
        output,
        frame,
        [1, 0, 0, -1, 0, 10],
        0.8,
        "© OpenStreetMap contributors",
      );
    expect(operations).toEqual([
      [background, 1],
      [frame.image, 0.8],
      [background, 1],
      [frame.image, 0.8],
    ]);
    expect(ctx.imageSmoothingEnabled).toBe(false);
    expect(ctx.fillText).toHaveBeenCalledWith(
      "KFTG · 2026-10-20 14:30:00 UTC",
      12,
      22,
    );
    expect(ctx.fillText).toHaveBeenCalledWith(
      "© OpenStreetMap contributors",
      12,
      758,
    );
  });
  it("rejects insufficient frames before touching the map", async () => {
    await expect(exportGIF({ scans: [] } as any)).rejects.toThrow(
      "At least two",
    );
  });
  it("produces GIF89a with multiple image blocks, 500ms delays and infinite looping", () => {
    const encoder = createEncoder();
    encoder.add(new Uint8ClampedArray([255, 0, 0, 255, 0, 0, 255, 255]), 2, 1);
    encoder.add(new Uint8ClampedArray([0, 255, 0, 255, 0, 0, 255, 255]), 2, 1);
    const bytes = encoder.finish();
    expect(new TextDecoder().decode(bytes.slice(0, 6))).toBe("GIF89a");
    expect([...bytes.slice(6, 10)]).toEqual([2, 0, 1, 0]);
    let offset = 13 + (bytes[10] & 128 ? 3 * 2 ** ((bytes[10] & 7) + 1) : 0);
    let images = 0;
    const delays: number[] = [];
    let loops = -1;
    const blocks = () => {
      while (bytes[offset]) offset += bytes[offset] + 1;
      offset++;
    };
    while (bytes[offset] !== 0x3b) {
      const marker = bytes[offset++];
      if (marker === 0x21) {
        const label = bytes[offset++];
        if (label === 0xf9)
          delays.push(bytes[offset + 2] + 256 * bytes[offset + 3]);
        if (label === 0xff)
          loops = bytes[offset + 14] + 256 * bytes[offset + 15];
        blocks();
      } else {
        expect(marker).toBe(0x2c);
        images++;
        const packed = bytes[offset + 8];
        offset += 9;
        if (packed & 128) offset += 3 * 2 ** ((packed & 7) + 1);
        offset++;
        blocks();
      }
      expect(offset).toBeLessThan(bytes.length);
    }
    expect(images).toBe(2);
    expect(delays).toEqual([50, 50]);
    expect(loops).toBe(0);
    expect(bytes.at(-1)).toBe(0x3b);
  });
});

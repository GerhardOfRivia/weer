import { GIFEncoder, quantize, applyPalette } from "gifenc";
export function createEncoder() {
  const gif = GIFEncoder();
  return {
    add(rgba: Uint8ClampedArray, width: number, height: number) {
      const palette = quantize(rgba, 256);
      gif.writeFrame(applyPalette(rgba, palette), width, height, {
        palette,
        delay: 500,
        repeat: 0,
      });
    },
    finish() {
      gif.finish();
      return gif.bytes();
    },
  };
}

import { createEncoder } from "./gif-encoder";
const encoder = createEncoder();
self.onmessage = ({ data }) => {
  try {
    if (data.type === "frame") {
      encoder.add(new Uint8ClampedArray(data.buffer), data.width, data.height);
      self.postMessage({ type: "ready" });
    } else {
      const bytes = encoder.finish();
      self.postMessage({ type: "done", bytes }, { transfer: [bytes.buffer] });
    }
  } catch (error) {
    self.postMessage({ type: "error", message: String(error) });
  }
};

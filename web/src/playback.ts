export type Scan = {
  id: string;
  station: string;
  state: string;
  frame_id: string;
  observation_time: string;
  error: string;
};
export function reconcile(
  scans: Scan[],
  selected: string,
  live: boolean,
): string {
  const ready = scans.filter((s) => s.state === "ready");
  if (live) return ready.at(-1)?.id ?? "";
  return selected; // An expired historical choice stays unavailable until the user chooses.
}
export function step(scans: Scan[], selected: string, delta: number): string {
  if (!scans.length) return "";
  const index = scans.findIndex((s) => s.id === selected);
  return scans[(Math.max(index, 0) + delta + scans.length) % scans.length].id;
}

import React, { useEffect, useRef, useState } from "react";
import { createRoot } from "react-dom/client";
import Map from "ol/Map";
import View from "ol/View";
import ImageLayer from "ol/layer/Image";
import ImageStatic from "ol/source/ImageStatic";
import TileLayer from "ol/layer/Tile";
import XYZ from "ol/source/XYZ";
import OSM from "ol/source/OSM";
import VectorLayer from "ol/layer/Vector";
import VectorSource from "ol/source/Vector";
import Feature from "ol/Feature";
import Point from "ol/geom/Point";
import { Circle, Fill, Stroke, Style, Text } from "ol/style";
import { fromLonLat } from "ol/proj";
import { reconcile, step, type Scan } from "./playback";
import { applyTheme, readTheme, saveTheme, type Theme } from "./theme";
import "ol/ol.css";
import "./style.css";
type Station = {
  id: string;
  name: string;
  latitude: number;
  longitude: number;
};
type Feed = {
  station: string;
  last_successful_source_check: string;
  source_error: string;
  latest_ready_observation: string;
  latest_discovered_nominal_time: string;
  pending: number;
  errors: {
    scan_id: string;
    message: string;
    stage: string;
    attempts: number;
    next_retry: string;
  }[];
};
type Status = {
  stations: Feed[];
  stale_after_seconds: number;
  retention_seconds: number;
  basemap_url: string;
  basemap_attribution: string;
  max_attempts: number;
};
type Manifest = {
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
const query = new URLSearchParams(location.search);
const preferredColorScheme = window.matchMedia("(prefers-color-scheme: dark)");
applyTheme(readTheme(), preferredColorScheme.matches);
async function get<T>(path: string, signal?: AbortSignal): Promise<T> {
  const r = await fetch(path, { signal });
  if (!r.ok) throw Error(`Request failed (${r.status})`);
  return r.json();
}
const age = (time: string, now: number) => {
  if (!time) return "unavailable";
  const minutes = Math.max(0, Math.floor((now - Date.parse(time)) / 60000));
  if (minutes >= 1440) return `${Math.floor(minutes / 1440)} days ago`;
  if (minutes >= 60) return `${Math.floor(minutes / 60)}h ${minutes % 60}m ago`;
  return `${minutes} min ago`;
};
function App() {
  const [theme, setTheme] = useState<Theme>(readTheme);
  const [stations, setStations] = useState<Station[]>([]),
    [station, setStation] = useState(query.get("station") ?? ""),
    [status, setStatus] = useState<Status | null>(null),
    [scans, setScans] = useState<Scan[]>([]),
    [selected, setSelected] = useState(query.get("scan") ?? ""),
    [live, setLive] = useState(!query.has("scan")),
    [playing, setPlaying] = useState(false),
    [speed, setSpeed] = useState(700),
    [opacity, setOpacity] = useState(0.8),
    [visible, setVisible] = useState(true),
    [panel, setPanel] = useState(true),
    [error, setError] = useState(""),
    [imageError, setImageError] = useState(""),
    [loading, setLoading] = useState(false),
    [manifest, setManifest] = useState<Manifest | null>(null),
    [now, setNow] = useState(Date.now());
  const element = useRef<HTMLDivElement>(null),
    map = useRef<Map | null>(null),
    radar = useRef(new ImageLayer({ opacity: 0.8 })),
    base = useRef(new TileLayer({ className: "ol-layer basemap-layer" })),
    markers = useRef(new VectorSource()),
    cache = useRef(
      new globalThis.Map<
        string,
        { image: HTMLImageElement; manifest: Manifest }
      >(),
    ),
    generation = useRef(0);
  const ready = scans.filter((s) => s.state === "ready"),
    current = ready.find((s) => s.id === selected),
    site = stations.find((s) => s.id === station),
    feed = status?.stations.find((s) => s.station === station),
    stale =
      !feed?.latest_ready_observation ||
      now - Date.parse(feed.latest_ready_observation) >
        (status?.stale_after_seconds ?? 900) * 1000;
  useEffect(() => {
    const update = () => applyTheme(theme, preferredColorScheme.matches);
    update();
    preferredColorScheme.addEventListener("change", update);
    return () => preferredColorScheme.removeEventListener("change", update);
  }, [theme]);
  useEffect(() => {
    get<Station[]>("/api/v1/stations")
      .then((s) => {
        setStations(s);
        setStation((old) =>
          s.some((x) => x.id === old) ? old : (s[0]?.id ?? ""),
        );
      })
      .catch((e) => setError(e.message));
    const t = setInterval(() => setNow(Date.now()), 10000);
    return () => clearInterval(t);
  }, []);
  useEffect(() => {
    if (!element.current) return;
    const m = new Map({
      target: element.current,
      layers: [
        base.current,
        radar.current,
        new VectorLayer({ source: markers.current }),
      ],
      view: new View({ center: fromLonLat([-104.545806, 39.786639]), zoom: 7 }),
    });
    map.current = m;
    return () => {
      radar.current.setSource(null);
      m.setTarget(undefined);
      m.dispose();
      cache.current.clear();
    };
  }, []);
  useEffect(() => {
    base.current.setSource(
      status?.basemap_url
        ? status.basemap_url === "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
          ? new OSM()
          : new XYZ({
              url: status.basemap_url,
              attributions: status.basemap_attribution,
              attributionsCollapsible: false,
            })
        : null,
    );
  }, [status?.basemap_url, status?.basemap_attribution]);
  useEffect(() => {
    markers.current.clear();
    for (const s of stations) {
      const f = new Feature(new Point(fromLonLat([s.longitude, s.latitude])));
      f.setStyle(
        new Style({
          image: new Circle({
            radius: s.id === station ? 6 : 4,
            fill: new Fill({ color: s.id === station ? "#fff" : "#8193a5" }),
            stroke: new Stroke({ color: "#10232d", width: 2 }),
          }),
          text: new Text({
            text: s.id,
            offsetY: 17,
            fill: new Fill({ color: "#fff" }),
            stroke: new Stroke({ color: "#10232d", width: 3 }),
          }),
        }),
      );
      markers.current.addFeature(f);
    }
    if (site)
      map.current
        ?.getView()
        .animate({
          center: fromLonLat([site.longitude, site.latitude]),
          duration: 250,
        });
  }, [stations, station]);
  useEffect(() => {
    if (!station) return;
    const controller = new AbortController();
    let active = true;
    setScans([]);
    setManifest(null);
    setPlaying(false);
    radar.current.setSource(null);
    generation.current++;
    setLoading(true);
    let polling = false;
    const poll = async () => {
      if(polling) return;
      polling = true;
      try {
        const st = await get<Status>("/api/v1/status", controller.signal);
        const params = new URLSearchParams({ limit: "1000" }); // Historical imported data can be selected by URL even outside the default recent window.
        const list = await get<Scan[]>(
          `/api/v1/stations/${station}/scans?${params}`,
          controller.signal,
        );
        if (active) {
          setStatus(st);
          setScans(list);
          setError("");
        }
      } catch (e) {
        if (active) setError((e as Error).message);
      } finally {
        polling = false;
        if (active) setLoading(false);
      }
    };
    void poll();
    const t = setInterval(poll, 15000);
    return () => {
      active = false;
      controller.abort();
      clearInterval(t);
    };
  }, [station]);
  useEffect(() => {
    setSelected((old) => reconcile(scans, old, live));
  }, [scans, live]);
  useEffect(() => {
    const q = new URLSearchParams();
    if (station) q.set("station", station);
    if (!live && selected) q.set("scan", selected);
    history.replaceState(null, "", `?${q}`);
  }, [station, selected, live]);
  useEffect(() => {
    radar.current.setOpacity(opacity);
    radar.current.setVisible(visible);
  }, [opacity, visible]);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    const gen = ++generation.current;
    setImageError("");
    setManifest(null);
    if (!current) {
      radar.current.setSource(null);
      return;
    }
    setLoading(true);
    async function load(s: Scan) {
      const old = cache.current.get(s.frame_id);
      if (old) return old;
      const m = await get<Manifest>(
        `/api/v1/frames/${s.frame_id}/manifest`,
        controller.signal,
      );
      if (m.station.id !== station) throw Error("Station mismatch");
      const img = new window.Image();
      img.src = `/api/v1/frames/${s.frame_id}/image.png`;
      await img.decode();
      if (controller.signal.aborted) throw Error("Cancelled");
      const entry = { image: img, manifest: m };
      cache.current.set(s.frame_id, entry);
      while (cache.current.size > 12)
        cache.current.delete(cache.current.keys().next().value!);
      return entry;
    }
    void load(current)
      .then(({ image, manifest: m }) => {
        if (!active || gen !== generation.current) return;
        const source = new ImageStatic({
          url: image.src,
          projection: m.projection,
          imageExtent: m.extent,
          interpolate: false,
          imageLoadFunction: (olImage) => {
            (olImage.getImage() as HTMLImageElement).src = image.src;
          },
        });
        radar.current.setSource(source);
        setManifest(m);
        setLoading(false);
        const i = ready.findIndex((s) => s.id === current.id);
        for (const other of ready.slice(Math.max(0, i - 1), i + 3))
          if (other.id !== current.id) void load(other).catch(() => {});
      })
      .catch((e) => {
        if (active) {
          radar.current.setSource(null);
          setImageError(`Image unavailable: ${e.message}`);
          setLoading(false);
        }
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [current?.frame_id, station]);
  useEffect(() => {
    if (!playing || ready.length < 2) return;
    const t = setInterval(
      () => setSelected((old) => step(ready, old, 1)),
      speed,
    );
    return () => clearInterval(t);
  }, [playing, ready.map((s) => s.id).join(","), speed]);
  const choose = (id: string) => {
    setLive(false);
    setSelected(id);
  };
  return (
    <main>
      <header>
        <button
          className="wordmark"
          onClick={() => setPanel(!panel)}
          aria-label="Toggle station settings"
        >
          ◉ weer
        </button>
        <span className="subtitle">NEXRAD / REFLECTIVITY</span>
        <span className={`badge ${stale || error || feed?.source_error || feed?.errors.length ? "warn" : ""}`}>
          {live ? "LIVE" : "HISTORY"} · {stale ? "FEED STALE" : "FEED CURRENT"}
          {(error || feed?.source_error || !!feed?.errors.length) && " · ERROR"}
        </span>
        <select
          className="theme-select"
          aria-label="Color theme"
          value={theme}
          onChange={(event) => {
            const nextTheme = event.target.value as Theme;
            setTheme(nextTheme);
            saveTheme(nextTheme);
          }}
        >
          <option value="system">System</option>
          <option value="light">Light</option>
          <option value="dark">Dark</option>
        </select>
      </header>
      <div
        ref={element}
        className={`map ${status?.basemap_url === "https://tile.openstreetmap.org/{z}/{x}/{y}.png" ? "osm-basemap" : ""}`}
      />
      {panel && (
        <aside>
          <small>RADAR STATION</small>
          <select
            value={station}
            onChange={(e) => {
              setStation(e.target.value);
              setLive(true);
              setSelected("");
            }}
          >
            {stations.map((s) => (
              <option key={s.id}>{s.id}</option>
            ))}
          </select>
          <h2>{site?.name ?? "Loading stations…"}</h2>
          <button
            onClick={() =>
              site &&
              map.current
                ?.getView()
                .animate({
                  center: fromLonLat([site.longitude, site.latitude]),
                  zoom: 7,
                  duration: 300,
                })
            }
          >
            ◎ Return to station
          </button>
          <hr />
          <label>
            <input
              type="checkbox"
              checked={visible}
              onChange={(e) => setVisible(e.target.checked)}
            />{" "}
            Radar visible
          </label>
          <label>
            Opacity <span>{Math.round(opacity * 100)}%</span>
            <input
              aria-label="Radar opacity"
              type="range"
              min="0"
              max="1"
              step=".05"
              value={opacity}
              onChange={(e) => setOpacity(+e.target.value)}
            />
          </label>
          <small>STATION FEED</small>
          <p>
            Latest rendered: {age(feed?.latest_ready_observation ?? "", now)}
            <br />
            Pending: {feed?.pending ?? 0}
            <br />
            Source checked: {age(feed?.last_successful_source_check ?? "", now)}
          </p>
          {!status?.basemap_url && (
            <p className="muted">
              No basemap · radar coordinates remain georeferenced.
            </p>
          )}
          {(error || feed?.source_error) && (
            <p role="alert" className="error">
              Source/API error: {error || feed?.source_error}
            </p>
          )}
          {feed?.errors.map((e) => (
            <details className="error" key={e.scan_id}>
              <summary>
                {e.stage === "discovered" ? "Download" : "Processing"} failure ·{" "}
                {e.attempts >= (status?.max_attempts ?? 4)
                  ? "retry limit reached"
                  : "retry pending"}
              </summary>
              {e.message}
            </details>
          ))}
        </aside>
      )}
      <section className="observation">
        <small>SELECTED OBSERVATION</small>
        {manifest ? (
          <>
            <strong>Reflectivity · {manifest.elevation.toFixed(2)}°</strong>
            <time>
              {new Date(manifest.sweep_time.start)
                .toISOString()
                .replace("T", " ")
                .replace("Z", " UTC")}
            </time>
            <span>
              {age(manifest.sweep_time.start, now)}
              {!manifest.has_valid_measurements
                ? " · Valid scan, all measurements masked"
                : ""}
            </span>
          </>
        ) : (
          <strong>
            {loading
              ? "Loading…"
              : current
                ? "Loading frame…"
                : "No available observation"}
          </strong>
        )}
        {imageError && (
          <p className="error" role="alert">
            {imageError}
          </p>
        )}
        {loading && manifest && <span>Loading next frame…</span>}
      </section>
      {manifest && (
        <div className="legend">
          <small>REFLECTIVITY / dBZ</small>
          <div>
            {manifest.color_scale.colors.map((c, i) => (
              <span
                key={c}
                style={{ background: c }}
                title={`${manifest.color_scale.thresholds[i]} dBZ`}
              />
            ))}
          </div>
          <p>
            {manifest.color_scale.thresholds
              .filter((_, i) => i % 3 === 0)
              .map((x) => (
                <span key={x}>{x}</span>
              ))}
          </p>
        </div>
      )}
      <footer>
        <div className="controls">
          <button
            disabled={!ready.length}
            onClick={() => choose(step(ready, selected, -1))}
            aria-label="Previous frame"
          >
            ⏮
          </button>
          <button
            disabled={ready.length < 2}
            onClick={() => {
              setLive(false);
              setPlaying(!playing);
            }}
          >
            {playing ? "Pause" : "Play"}
          </button>
          <button
            disabled={!ready.length}
            onClick={() => choose(step(ready, selected, 1))}
            aria-label="Next frame"
          >
            ⏭
          </button>
          <select
            aria-label="Playback speed"
            value={speed}
            onChange={(e) => setSpeed(+e.target.value)}
          >
            <option value={1400}>0.5×</option>
            <option value={700}>1×</option>
            <option value={350}>2×</option>
          </select>
          <input
            aria-label="Observation timeline"
            type="range"
            min={0}
            max={Math.max(0, ready.length - 1)}
            value={Math.max(
              0,
              ready.findIndex((s) => s.id === selected),
            )}
            disabled={!ready.length}
            onChange={(e) => {
              setPlaying(false);
              choose(ready[+e.target.value].id);
            }}
          />
          <button
            className={live ? "active" : ""}
            onClick={() => {
              setPlaying(false);
              setLive(true);
            }}
          >
            Live
          </button>
          <span>{ready.length} frames</span>
        </div>
        <div className="times">
          <span>{ready[0]?.observation_time}</span>
          <span>{ready.at(-1)?.observation_time}</span>
        </div>
        <p className="notice">
          Independent visualization of NOAA data. Frames are processed output;
          no NOAA endorsement. Not an official warning service or a sole source
          for safety-critical decisions.
        </p>
      </footer>
    </main>
  );
}
createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
);

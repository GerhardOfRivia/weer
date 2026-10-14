export type Theme = "system" | "light" | "dark";

export function readTheme(): Theme {
  try {
    const saved = localStorage.getItem("weer-theme");
    if (saved === "light" || saved === "dark") return saved;
  } catch {}
  return "system";
}

export function applyTheme(theme: Theme, prefersDark: boolean) {
  document.documentElement.dataset.theme =
    theme === "system" ? (prefersDark ? "dark" : "light") : theme;
}

export function saveTheme(theme: Theme) {
  try {
    localStorage.setItem("weer-theme", theme);
  } catch {}
}

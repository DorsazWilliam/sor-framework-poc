import type { Wrap } from "../api/contract";

export type ScreenState = { params?: Record<string, unknown>; wrap?: Wrap; widths?: Record<string, number> };

// visit counts the navigations (hash changes): a screen is mounted again with the state of the new URL.
export const route = $state({ screen: "", state: {} as ScreenState, visit: 0 });

const href = (screen: string, state: ScreenState) =>
  `#/${screen}` + (Object.keys(state).length ? `?s=${encodeURIComponent(JSON.stringify(state))}` : "");

function read() {
  const m = /^#\/([^?]*)(?:\?s=(.*))?$/.exec(location.hash);
  route.screen = m?.[1] ?? "";
  route.visit++;
  try {
    route.state = m?.[2] ? JSON.parse(decodeURIComponent(m[2])) : {};
  } catch {
    route.state = {};
  }
}

read();
addEventListener("hashchange", read);

export function go(screen: string, state: ScreenState = {}) {
  location.hash = href(screen, state);
}

export function keep(state: ScreenState) {
  history.replaceState(null, "", href(route.screen, state));
}

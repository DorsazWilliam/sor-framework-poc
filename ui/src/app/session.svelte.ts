import type { Command, Lang, Me, Meta, Screen, Table, View } from "../api/contract";
import { api } from "../api/client";
import { has, setLang, t } from "../labels/i18n.svelte";

export const session = $state({
  me: null as unknown as Me,
  meta: null as unknown as Meta,
  theme: "light" as "light" | "dark",
  status: { rows: 0, selected: 0 },
});

const index = { tables: new Map<string, Table>(), views: new Map<string, View>(), commands: new Map<string, Command>() };

export const table = (name: string) => index.tables.get(name);
export const view = (name: string) => index.views.get(name);
export const command = (name: string) => index.commands.get(name);
// The meta holds what the principal's capability permits (SPEC §8): a screen's view is its first one.
export const screenView = (s: Screen) => view(s.views[0])!;

// A screen's label: s.<id>, else the label of its table (SPEC §19).
export function screenLabel(s: Screen) {
  return s.table && !has(`s.${s.id}`) ? t(`t.${s.table}`) : t(`s.${s.id}`);
}

export function colLabel(name: string, tbl?: string) {
  if (tbl && has(`c.${tbl}.${name}`)) return t(`c.${tbl}.${name}`);
  return has(`c.${name}`) ? t(`c.${name}`) : name;
}

export const stored = (k: string) => {
  try {
    return localStorage.getItem(k);
  } catch {
    return null;
  }
};
export const store = (k: string, v: string) => {
  try {
    localStorage.setItem(k, v);
  } catch {
    // storage unavailable: preference lasts for the session
  }
};

export function setTheme(theme: "light" | "dark") {
  session.theme = theme;
  document.documentElement.dataset.theme = theme;
  store("theme", theme);
}

export async function changeLang(lang: Lang) {
  await setLang(lang);
  store("lang", lang);
}

export async function boot() {
  const [me, meta] = await Promise.all([api.me(), api.meta()]);
  for (const x of meta.tables) index.tables.set(x.name, x);
  for (const x of meta.views) index.views.set(x.name, x);
  for (const x of meta.commands) index.commands.set(x.name, x);
  session.me = me;
  session.meta = meta;
  const theme = stored("theme");
  setTheme(theme === "dark" || theme === "light" ? theme : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  await setLang((stored("lang") as Lang | null) ?? me.lang);
}

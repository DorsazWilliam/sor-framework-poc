import type { Command, Lang, Me, Meta, Screen, Table, View } from "../api/contract";
import { api } from "../api/client";
import { begin, clear, remember, restore, type Boot } from "./cache.svelte";
import { has, i18n, setLabels, setLang, t } from "../labels/i18n.svelte";

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
  remember($state.snapshot({ me: session.me, meta: session.meta, lang, labels: i18n.registry }));
}

function start(b: Boot) {
  for (const x of b.meta.tables) index.tables.set(x.name, x);
  for (const x of b.meta.views) index.views.set(x.name, x);
  for (const x of b.meta.commands) index.commands.set(x.name, x);
  session.me = b.me;
  session.meta = b.meta;
  setLabels(b.lang, b.labels);
  begin(b.me, b.meta);
  remember(b);
}

// From the tab's cache at once when it holds a boot, then read again: another identity or registry starts over.
export async function boot() {
  const theme = stored("theme");
  setTheme(theme === "dark" || theme === "light" ? theme : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  const kept = restore();
  if (kept) {
    start(kept);
    Promise.all([api.me(), api.meta(), api.labels(kept.lang)])
      .then(([me, meta, labels]) => {
        if (JSON.stringify(me) !== JSON.stringify(kept.me) || meta.version !== kept.meta.version) throw new Error("changed");
        setLabels(kept.lang, labels);
        remember({ ...kept, labels });
      })
      .catch(() => {
        clear();
        location.reload();
      });
    return;
  }
  clear();
  const [me, meta] = await Promise.all([api.me(), api.meta()]);
  const lang = (stored("lang") as Lang | null) ?? me.lang;
  start({ me, meta, lang, labels: await api.labels(lang) });
}

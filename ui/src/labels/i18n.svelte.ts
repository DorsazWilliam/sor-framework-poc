import type { Labels, Lang } from "../api/contract";
import { api } from "../api/client";
import { fmtNumber } from "../ds/format";
import fr from "./fr.json";
import de from "./de.json";
import en from "./en.json";

const chrome: Record<Lang, Labels> = { fr, de, en };

export const i18n = $state({ lang: "fr" as Lang, registry: {} as Labels });

export function has(key: string) {
  return key in i18n.registry || key in chrome[i18n.lang] || key in chrome.fr;
}

export function t(key: string, vars?: Record<string, unknown>): string {
  const s = i18n.registry[key] ?? chrome[i18n.lang][key] ?? chrome.fr[key] ?? key;
  return vars ? s.replace(/\{(\w+)\}/g, (_, k) => String(vars[k] ?? "")) : s;
}

// A count: the label <key>.<plural category of n> (one, other, …), with {n} formatted.
export function tn(key: string, n: number): string {
  const k = `${key}.${new Intl.PluralRules(i18n.lang).select(n)}`;
  return t(has(k) ? k : `${key}.other`, { n: fmtNumber(n) });
}

export function setLabels(lang: Lang, registry: Labels) {
  i18n.registry = registry;
  i18n.lang = lang;
  document.documentElement.lang = lang;
}

export const setLang = async (lang: Lang) => setLabels(lang, await api.labels(lang));

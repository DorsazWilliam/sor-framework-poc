import type { Labels, Lang } from "../api/contract";
import { api } from "../api/client";
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

export async function setLang(lang: Lang) {
  i18n.registry = await api.labels(lang);
  i18n.lang = lang;
  document.documentElement.lang = lang;
}

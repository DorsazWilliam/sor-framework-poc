import type { Col } from "../api/contract";
import { fmtDate, fmtDuration, fmtInstant, fmtMoney, fmtNumber, fmtTime } from "../ds/format";
import { t } from "../labels/i18n.svelte";
import { lookup } from "./lookups.svelte";

const numericTypes = new Set(["int2", "int4", "int8", "numeric", "float4", "float8"]);

export const isNumeric = (c: Col) => numericTypes.has(c.type) && !c.ref && !c.enum;

// Postgres int4range text: "[480,720)" → [480, 720] (exclusive upper bound)
export function parseRange(v: string): [number | null, number | null] {
  const m = /^[[(](\d*),(\d*)[\])]$/.exec(v);
  return m ? [m[1] ? +m[1] : null, m[2] ? +m[2] : null] : [null, null];
}

export function display(c: Col, v: unknown): string {
  if (v == null || v === "") return "";
  if (c.ref) return lookup(c.ref, v as number) ?? "";
  if (c.enum) return t(`e.${c.enum}.${v}`);
  if (c.format === "min") return Array.isArray(v) ? v.map(fmtDuration).join("  ") : fmtDuration(v as number);
  if (c.format === "tod") {
    if (typeof v !== "string") return fmtTime(v as number);
    const [a, b] = parseRange(v);
    return `${a == null ? "" : fmtTime(a)}–${b == null ? "" : fmtTime(b)}`;
  }
  if (c.format === "money") return fmtMoney(v as number);
  if (c.format === "pct") return `${fmtNumber(v as number)} %`;
  if (c.type === "date") return fmtDate(v as string);
  if (c.type === "timestamptz") return fmtInstant(v as string);
  if (numericTypes.has(c.type)) return fmtNumber(v as number);
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

export function defaultWidth(c: Col) {
  if (c.ref) return 180;
  if (c.type === "bool") return 64;
  if (c.type === "date") return 96;
  if (c.type === "timestamptz") return 140;
  if (c.format === "tod") return c.type === "int4range" ? 112 : 64;
  if (c.format === "min") return c.type.startsWith("_") ? 220 : 88;
  if (c.format === "money") return 112;
  if (c.enum) return 120;
  if (numericTypes.has(c.type)) return 88;
  return 220;
}

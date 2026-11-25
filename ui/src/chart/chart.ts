import { scaleLinear } from "d3-scale";
import type { Attachment } from "svelte/attachments";
import { on } from "svelte/events";
import type { Row } from "../api/contract";

// What a chart draws: its rows, the key of their axis (or category) and the keys of their values, with their formats.
export type Axis = { key: string; fmt: (v: unknown) => string };
// `unit`: the step ticks are counted in (60 for minutes: ticks on whole hours).
export type Series = { key: string; label: string; fmt: (v: number) => string; tick: (v: number) => string; unit?: number };
export type Pick = (row: Row) => void;
export type Tip = { text: string; x: number; y: number };
// A mark of a chart: focusable, named by its tooltip, shown on hover and focus, picked by click or Enter.
export type Mark = (text: string, x: number, y: number, pick?: () => void) => Attachment<SVGElement>;

export const color = (i: number) => `var(--chart-${(i % 6) + 1})`;
export const num = (r: Row, key: string) => Number(r[key] ?? 0);
// Width of a label in the 11 px caption font, for margins and label thinning.
export const textWidth = (s: string) => s.length * 6.2;
export const clip = (s: string, w: number) => (textWidth(s) <= w ? s : `${s.slice(0, Math.max(1, Math.floor(w / 6.2) - 1))}…`);

// A value scale over the values and 0, with about five ticks in the unit of the first series.
export function valueScale(values: number[], s?: Series) {
  const u = s?.unit ?? 1;
  const nice = scaleLinear()
    .domain([Math.min(0, ...values) / u, Math.max(0, ...values, 1e-9) / u])
    .nice(5);
  return {
    s: scaleLinear().domain(nice.domain().map((d) => d * u)),
    ticks: nice.ticks(5).map((v) => ({ v: v * u, label: s ? s.tick(v * u) : String(v * u) })),
  };
}

export function marker(show: (t: Tip | null) => void): Mark {
  return (text, x, y, pick) => (el) => {
    el.setAttribute("tabindex", "0");
    el.setAttribute("role", "graphics-symbol");
    el.setAttribute("aria-label", text);
    if (pick) el.style.cursor = "pointer";
    const tip = () => show({ text, x, y });
    const hide = () => show(null);
    const offs = [
      on(el, "pointerenter", tip),
      on(el, "focus", tip),
      on(el, "pointerleave", hide),
      on(el, "blur", hide),
      on(el, "click", () => pick?.()),
      on(el, "keydown", (e) => {
        if (pick && ["Enter", " "].includes((e as KeyboardEvent).key)) {
          e.preventDefault();
          pick();
        }
      }),
    ];
    return () => offs.forEach((off) => off());
  };
}

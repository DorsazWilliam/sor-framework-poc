const money = new Intl.NumberFormat("de-CH", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
const number = new Intl.NumberFormat("de-CH", { maximumFractionDigits: 4 });
const date = new Intl.DateTimeFormat("fr-CH", { day: "2-digit", month: "2-digit", year: "numeric" });
const instant = new Intl.DateTimeFormat("fr-CH", { dateStyle: "short", timeStyle: "short" });

const pad = (n: number) => String(n).padStart(2, "0");

export const fmtMoney = (n: number) => money.format(n);
export const fmtNumber = (n: number) => number.format(n);

export function fmtDuration(min: number) {
  const a = Math.abs(Math.round(min));
  return `${min < 0 ? "-" : ""}${number.format(Math.floor(a / 60))}:${pad(a % 60)}`;
}

export const fmtTime = (m: number) => `${pad(Math.floor(m / 60))}:${pad(m % 60)}`;

export function fmtDate(iso: string) {
  const [y, m, d] = iso.split("-").map(Number);
  return date.format(new Date(y, m - 1, d));
}

export const fmtInstant = (iso: string) => instant.format(new Date(iso));

export const isoDate = (d: Date) => `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;

// Parsers return null for empty input and undefined for invalid input.
export function parseNumber(s: string): number | null | undefined {
  const v = s.trim().replace(/['’\s]/g, "").replace(",", ".");
  if (!v) return null;
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}

export function parseDuration(s: string): number | null | undefined {
  const v = s.trim().toLowerCase().replace(",", ".").replace(/['’]/g, "");
  if (!v) return null;
  let m =/^(\d+):(\d{1,2})$/.exec(v);
  if (m) return +m[2] < 60 ? +m[1] * 60 + +m[2] : undefined;
  m = /^(\d+(?:\.\d+)?)m$/.exec(v);
  if (m) return Math.round(+m[1]);
  m = /^(\d+(?:\.\d+)?)h?$/.exec(v);
  return m ? Math.round(+m[1] * 60) : undefined;
}

// dd.mm.yyyy, dd.mm.yy, dd.mm (current year) or ISO → ISO date
export function parseDateText(s: string): string | null | undefined {
  const v = s.trim();
  if (!v) return null;
  if (/^\d{4}-\d{2}-\d{2}$/.test(v)) return v;
  const m = /^(\d{1,2})\.(\d{1,2})\.?(\d{2}|\d{4})?$/.exec(v);
  if (!m) return undefined;
  const y = m[3] ? (m[3].length === 2 ? 2000 + +m[3] : +m[3]) : new Date().getFullYear();
  const d = new Date(y, +m[2] - 1, +m[1]);
  return d.getMonth() === +m[2] - 1 ? isoDate(d) : undefined;
}

export function parseTime(s: string): number | null | undefined {
  const v = s.trim().toLowerCase();
  if (!v) return null;
  const m = /^(\d{1,2})(?:[:h.]?(\d{2}))?$/.exec(v);
  if (!m) return undefined;
  const t = +m[1] * 60 + +(m[2] ?? 0);
  return +(m[2] ?? 0) < 60 && t <= 1440 ? t : undefined;
}

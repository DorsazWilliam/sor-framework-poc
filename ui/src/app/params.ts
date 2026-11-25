import type { Me } from "../api/contract";
import { isoDate } from "../ds/format";

// Screen defaults: $today, $month_start, $year_start, $days(n), $self; anything else is a literal.
export function resolve(expr: string, me: Me): unknown {
  const d = new Date();
  if (expr === "$today") return isoDate(d);
  if (expr === "$month_start") return isoDate(new Date(d.getFullYear(), d.getMonth(), 1));
  if (expr === "$year_start") return isoDate(new Date(d.getFullYear(), 0, 1));
  if (expr === "$self") return me.id;
  const m = /^\$days\((-?\d+)\)$/.exec(expr);
  if (m) return isoDate(new Date(d.getFullYear(), d.getMonth(), d.getDate() + +m[1]));
  return expr;
}

export function defaults(map: Record<string, string>, me: Me) {
  return Object.fromEntries(Object.entries(map).map(([k, v]) => [k, resolve(v, me)]));
}

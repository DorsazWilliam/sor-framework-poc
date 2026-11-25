import type { Filter, Me, View } from "../api/contract";
import { isoDate } from "../ds/format";

// Screen defaults: $today, $month_start, $year_start, $days(n), $self; anything else is a literal.
export function resolve(expr: string, me: Me): unknown {
  const d = new Date();
  if (expr === "$today") return isoDate(d);
  if (expr === "$month_start") return isoDate(new Date(d.getFullYear(), d.getMonth(), 1));
  if (expr === "$year_start") return isoDate(new Date(d.getFullYear(), 0, 1));
  if (expr === "$self") return me.self;
  const m = /^\$days\((-?\d+)\)$/.exec(expr);
  if (m) return isoDate(new Date(d.getFullYear(), d.getMonth(), d.getDate() + +m[1]));
  return expr;
}

// A child screen's link values (SPEC §19): a key that is a param of the view fills it, any other key is an
// equality filter on that column of the view.
export function linkWrap(v: View, link: Record<string, unknown>) {
  const params: Record<string, unknown> = {};
  const filter: Filter[] = [];
  for (const [k, value] of Object.entries(link)) {
    if (v.params.some((p) => p.name === k)) params[k] = value;
    else filter.push({ col: k, op: "eq", value });
  }
  return { params, filter };
}

export function defaults(map: Record<string, string>, me: Me) {
  return Object.fromEntries(Object.entries(map).map(([k, v]) => [k, resolve(v, me)]));
}

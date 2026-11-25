import type { Changed, Command, Rejected, Row } from "../api/contract";
import { api, ApiError } from "../api/client";
import { own } from "../app/live.svelte";
import { table } from "../app/session.svelte";
import { toast } from "../ds/toast.svelte";
import { t } from "../labels/i18n.svelte";
import type { Icon } from "../ds/types";
import Add from "~icons/fluent/add-16-regular";
import Edit from "~icons/fluent/edit-16-regular";
import Delete from "~icons/fluent/delete-16-regular";
import Play from "~icons/fluent/play-16-regular";
import Flash from "~icons/fluent/flash-16-regular";

export const kindIcon: Record<Command["kind"], Icon> = { create: Add, update: Edit, delete: Delete, run: Play, custom: Flash };

export const takesIds = (c: Command) => !c.rootless && c.kind !== "create" && c.kind !== "run";

// Enabled = action true (the meta holds the permitted commands only); otherwise the reasons, as labels. The action is the root's:
// read on the rows for a root table, on the parent row for a child table (SPEC §7).
export function evaluate(c: Command, rows: Row[], parent?: Row): { enabled: boolean; reasons: string[] } {
  if (takesIds(c) && !rows.length) return { enabled: false, reasons: [t("ui.select_rows")] };
  const tbl = table(c.table);
  const conds = (c.when && tbl && table(tbl.root)?.actions[c.when]) || [];
  const on = tbl && tbl.root !== tbl.name ? (parent ? [parent] : []) : rows;
  const failed = conds.filter((k) => on.some((r) => r[k] === false));
  return { enabled: !failed.length, reasons: failed.length ? [t("ui.unmet", { list: failed.map((k) => t(`cond.${k}`)).join(", ") })] : [] };
}

// The command and its own refresh through the live store; an ApiError is returned, not thrown.
export async function call<K extends string>(name: K, input: Parameters<typeof api.call<K>>[1]): Promise<Changed | ApiError> {
  try {
    return await own(() => api.call(name, input));
  } catch (e) {
    if (!(e instanceof ApiError)) throw e;
    return e;
  }
}

export async function execute(
  c: Command,
  input: Record<string, unknown>,
  onRejected?: (r: Rejected[]) => void,
): Promise<Changed | ApiError> {
  const res = await call(c.name, input);
  if (res instanceof ApiError) {
    report(c, res);
    if (res.problem.rejected) onRejected?.(res.problem.rejected);
  }
  return res;
}

export function report(c: Command, e: ApiError) {
  const p = e.problem;
  const name = t(`cmd.${c.name}`);
  if (p.code === "rejected") {
    const conds = [...new Set(p.rejected?.flatMap((r) => r.conditions))];
    toast("error", t("ui.rejected", { cmd: name, n: p.rejected?.length ?? 0 }), conds.map((k) => t(`cond.${k}`)));
  } else if (p.code === "constraint") toast("error", t(`constraint.${p.constraint}`));
  else if (p.code === "forbidden") toast("error", t("ui.forbidden"), p.needs ? [t(`fn.${p.needs}`)] : undefined);
  else if (p.code === "not_found") toast("error", t("ui.not_found"));
  else toast("error", p.detail ?? p.title, (p.errors as { message?: string }[] | undefined)?.map((x) => x.message ?? ""));
}

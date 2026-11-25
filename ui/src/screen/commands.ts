import type { Changed, Command, Rejected, Row } from "../api/contract";
import { api, ApiError } from "../api/client";
import { applied } from "../app/live.svelte";
import { can, table } from "../app/session.svelte";
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

// Conditions of an action; a child table's commands use its root's actions (SPEC §7).
function conditions(c: Command) {
  const tbl = table(c.table);
  if (!c.when || !tbl) return [];
  return tbl.actions[c.when] ?? table(tbl.root)?.actions[c.when] ?? [];
}

// Enabled = capability ∧ action true on every selected row; otherwise the reasons, as labels.
export function evaluate(c: Command, rows: Row[], parent?: Row): { enabled: boolean; reasons: string[] } {
  if (!can(c.needs)) return { enabled: false, reasons: [t("ui.forbidden")] };
  if (takesIds(c) && !rows.length) return { enabled: false, reasons: [t("ui.select_rows")] };
  const failed = conditions(c).filter((k) => rows.some((r) => (r[k] ?? parent?.[k]) === false));
  return { enabled: !failed.length, reasons: failed.length ? [t("ui.unmet", { list: failed.map((k) => t(`cond.${k}`)).join(", ") })] : [] };
}

export async function execute(
  c: Command,
  input: Record<string, unknown>,
  onRejected?: (r: Rejected[]) => void,
): Promise<Changed | ApiError> {
  try {
    const res = await api.call(c.name, input);
    applied(res);
    return res;
  } catch (e) {
    if (!(e instanceof ApiError)) throw e;
    report(c, e);
    if (e.problem.rejected) onRejected?.(e.problem.rejected);
    return e;
  }
}

function report(c: Command, e: ApiError) {
  const p = e.problem;
  const name = t(`cmd.${c.name}`);
  if (p.code === "rejected") {
    const conds = [...new Set(p.rejected?.flatMap((r) => r.conditions))];
    toast("error", t("ui.rejected", { cmd: name, n: p.rejected?.length ?? 0 }), conds.map((k) => t(`cond.${k}`)));
  } else if (p.code === "constraint") toast("error", t(`constraint.${p.constraint}`));
  else if (p.code === "forbidden") toast("error", t("ui.forbidden"), p.needs ? [t(`fn.${p.needs}`)] : undefined);
  else if (p.code === "not_found") toast("error", t("ui.not_found"));
  else toast("error", p.title, (p.errors as { message?: string }[] | undefined)?.map((x) => x.message ?? ""));
}

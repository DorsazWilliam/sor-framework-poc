<script lang="ts">
  import type { Col, Filter, Op } from "../api/contract";
  import Select from "../ds/Select.svelte";
  import Combobox from "../ds/Combobox.svelte";
  import { parseDateText, parseDuration, parseNumber, parseTime } from "../ds/format";
  import { display, isNumeric } from "../app/display";
  import { lookup, search } from "../app/lookups.svelte";
  import { session } from "../app/session.svelte";
  import { t } from "../labels/i18n.svelte";

  let { col, filter, onchange }: { col: Col; filter?: Filter; onchange: (f: Filter | null) => void } = $props();

  const ops: Record<string, Op> = { ">=": "ge", "<=": "le", "!=": "ne", ">": "gt", "<": "lt", "=": "eq" };
  const prefix = Object.fromEntries(Object.entries(ops).map(([k, v]) => [v, k]));
  const typed = $derived(isNumeric(col) || col.type === "date");

  function parse(text: string): Filter | null | undefined {
    const m = /^(>=|<=|!=|>|<|=)?\s*(.*)$/.exec(text.trim())!;
    if (!m[2]) return null;
    if (!typed) return { col: col.name, op: m[1] ? ops[m[1]] : "contains", value: m[2] };
    const p =
      col.type === "date"
        ? parseDateText
        : col.format === "min"
          ? parseDuration
          : col.format === "tod"
            ? parseTime
            : parseNumber;
    const v = p(m[2]);
    return v == null ? undefined : { col: col.name, op: m[1] ? ops[m[1]] : "eq", value: v };
  }

  const show = (f?: Filter) =>
    f ? (f.op === "contains" || (f.op === "eq" && typed) ? "" : prefix[f.op]) + (typed ? display(col, f.value) : String(f.value)) : "";

  let text = $state("");
  let invalid = $state(false);

  $effect(() => {
    text = show(filter);
  });

  function commit() {
    const f = parse(text);
    invalid = f === undefined;
    if (f !== undefined && show(f ?? undefined) !== show(filter)) onchange(f);
  }

  const choices = $derived(
    col.type === "bool"
      ? [
          { value: "", label: "" },
          { value: "true", label: t("ui.yes") },
          { value: "false", label: t("ui.no") },
        ]
      : [{ value: "", label: "" }, ...(session.meta.enums[col.enum ?? ""] ?? []).map((v) => ({ value: v, label: t(`e.${col.enum}.${v}`) }))],
  );
  const eq = (v: unknown) => onchange(v === "" || v == null ? null : { col: col.name, op: "eq", value: v });
</script>

{#if col.enum || col.type === "bool"}
  <Select
    items={choices}
    value={filter ? String(filter.value) : ""}
    placeholder={t("ui.filter_hint")}
    onValueChange={(v) => eq(col.type === "bool" ? (v === "" ? "" : v === "true") : v)}
  />
{:else if col.ref}
  <Combobox
    value={filter ? String(filter.value) : null}
    label={filter ? lookup(col.ref, filter.value as number) : ""}
    search={(q) => search(col.ref!, q)}
    placeholder={t("ui.filter_hint")}
    onValueChange={(v) => eq(v == null ? null : +v)}
  />
{:else}
  <input
    class="input"
    class:num={typed}
    aria-invalid={invalid}
    aria-label={t("ui.filter")}
    placeholder={t("ui.filter_hint")}
    bind:value={text}
    onblur={commit}
    onkeydown={(e) => e.key === "Enter" && commit()}
  />
{/if}

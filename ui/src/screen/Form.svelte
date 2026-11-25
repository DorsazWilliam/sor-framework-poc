<script lang="ts">
  import type { Row, Table } from "../api/contract";
  import Button from "../ds/Button.svelte";
  import Field from "../ds/Field.svelte";
  import Tooltip from "../ds/Tooltip.svelte";
  import FieldEditor from "./FieldEditor.svelte";
  import Lock from "~icons/fluent/lock-closed-16-regular";
  import { evaluate, execute } from "./commands";
  import { colLabel, command } from "../app/session.svelte";
  import { toast } from "../ds/toast.svelte";
  import { t } from "../labels/i18n.svelte";
  import { ApiError } from "../api/client";

  // Create (row = null) or edit form generated from the table's columns; update sends changed fields only.
  let {
    tbl,
    row,
    initial = {},
    file,
    parent,
    onsaved,
  }: {
    tbl: Table;
    row: Row | null;
    initial?: Row;
    file?: { view: string; params: Record<string, unknown> };
    parent?: Row;
    onsaved: (id: number) => void;
  } = $props();

  const cols = $derived(tbl.columns.filter((c) => c.name !== "id" && (!c.guarded || c.ref === "file")));
  const base = $derived(row ?? initial);
  let values = $state<Row>({});
  let errors = $state<Record<string, string>>({});
  let busy = $state(false);

  $effect(() => {
    values = structuredClone($state.snapshot(base));
    errors = {};
  });

  const cmd = $derived(command(`${tbl.name}.${row ? "update" : "create"}`));
  const status = $derived(cmd ? evaluate(cmd, row ? [row] : [], parent) : { enabled: false, reasons: [] });
  const changed = $derived(cols.filter((c) => !c.guarded && JSON.stringify(values[c.name] ?? null) !== JSON.stringify(base[c.name] ?? null)));

  async function save(e: Event) {
    e.preventDefault();
    if (!cmd) return;
    errors = Object.fromEntries(
      cols
        .filter((c) => !c.guarded && !c.nullable && !c.has_default && values[c.name] == null)
        .map((c) => [c.name, t("ui.required")]),
    );
    if (Object.keys(errors).length) return;
    // Create sends every value (prefilled ones included), update the changed ones.
    const sent = row ? changed : cols.filter((c) => !c.guarded && values[c.name] != null);
    const diff = Object.fromEntries(sent.map((c) => [c.name, values[c.name] ?? null]));
    busy = true;
    const res = await execute(cmd, row ? { ids: [row.id], ...diff } : diff);
    busy = false;
    if (res instanceof ApiError) {
      for (const x of (res.problem.errors ?? []) as { location?: string; message?: string }[]) {
        const k = x.location?.split(".").pop();
        if (k) errors[k] = x.message ?? "";
      }
      return;
    }
    toast("success", t("ui.saved"));
    onsaved(res.ids[0] ?? (row?.id as number));
  }
</script>

<form class="form" onsubmit={save}>
  {#if row && !status.enabled}
    <div class="note"><Lock />{t("ui.read_only", { reasons: status.reasons.join(", ") })}</div>
  {/if}
  <div class="fields">
    {#each cols as c (c.name)}
      <Field label={colLabel(c.name, tbl.name)} id="f-{c.name}" error={errors[c.name]}>
        <FieldEditor
          col={c}
          id="f-{c.name}"
          bind:value={values[c.name]}
          readonly={c.guarded || !status.enabled}
          file={file && row ? { ...file, id: row.id as number, column: c.name } : undefined}
        />
      </Field>
    {/each}
  </div>
  <footer>
    <Tooltip text={status.enabled ? "" : status.reasons.join(", ")}>
      <Button variant="primary" type="submit" disabled={!status.enabled || busy || (row != null && !changed.length)}>
        {t(row ? "ui.save" : "ui.create")}
      </Button>
    </Tooltip>
    {#if changed.length && row}
      <Button onclick={() => (values = structuredClone($state.snapshot(base)))}>{t("ui.discard")}</Button>
    {/if}
  </footer>
</form>

<style>
  .form {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
  }
  .note {
    display: flex;
    align-items: center;
    gap: var(--s-4);
    margin: var(--panel-pad) var(--panel-pad) 0;
    padding: var(--s-3) var(--s-5);
    border: var(--bw) solid var(--border);
    border-radius: var(--r-button);
    background: var(--surface-subtle);
    color: var(--text-muted);
  }
  .fields {
    flex: 1;
    overflow: auto;
    padding: var(--panel-pad);
  }
  footer {
    display: flex;
    gap: var(--s-4);
    padding: var(--s-5) var(--panel-pad);
    border-top: var(--bw) solid var(--border);
  }
</style>

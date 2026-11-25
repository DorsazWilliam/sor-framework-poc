<script lang="ts">
  import type { Col } from "../api/contract";
  import Input from "../ds/Input.svelte";
  import NumberInput from "../ds/NumberInput.svelte";
  import DurationInput from "../ds/DurationInput.svelte";
  import TimeInput from "../ds/TimeInput.svelte";
  import DateInput from "../ds/DateInput.svelte";
  import Checkbox from "../ds/Checkbox.svelte";
  import Select from "../ds/Select.svelte";
  import Combobox from "../ds/Combobox.svelte";
  import FileField from "./FileField.svelte";
  import { display, isNumeric, parseRange } from "../app/display";
  import { lookup, search } from "../app/lookups.svelte";
  import { session } from "../app/session.svelte";
  import { t } from "../labels/i18n.svelte";

  let {
    col,
    value = $bindable(),
    id,
    readonly = false,
    file,
  }: {
    col: Col;
    value?: unknown;
    id: string;
    readonly?: boolean;
    file?: { view: string; params: Record<string, unknown>; id: number; column: string };
  } = $props();

  const enumItems = $derived([
    ...(col.nullable ? [{ value: "", label: "" }] : []),
    ...(session.meta.enums[col.enum ?? ""] ?? []).map((v) => ({ value: v, label: t(`e.${col.enum}.${v}`) })),
  ]);

  const range = $derived(typeof value === "string" ? parseRange(value) : [null, null]);
  const setRange = (i: 0 | 1, v: number | null) => {
    const r = [...range];
    r[i] = v;
    value = r[0] == null && r[1] == null ? null : `[${r[0] ?? ""},${r[1] ?? ""})`;
  };

  const list = $derived(Array.isArray(value) ? (value as (number | null)[]) : []);
  const setAt = (i: number, v: number | null) => {
    const next = [...list];
    next[i] = v;
    while (next.length && next[next.length - 1] == null) next.pop();
    value = next.length ? next : null;
  };
</script>

{#if col.ref === "file"}
  <FileField bind:value={value as number | null} source={file} {readonly} {id} />
{:else if readonly}
  <input class="input" class:num={isNumeric(col)} {id} readonly value={display(col, value)} />
{:else if col.ref}
  <Combobox
    {id}
    bind:value={() => (value == null ? null : String(value)), (v) => (value = v == null ? null : +v)}
    label={value == null ? "" : lookup(col.ref, value as number)}
    search={(q) => search(col.ref!, q)}
  />
{:else if col.enum}
  <Select {id} items={enumItems} bind:value={() => (value as string) ?? "", (v) => (value = v || null)} />
{:else if col.type === "bool"}
  <Checkbox {id} bind:checked={() => !!value, (v) => (value = v)} />
{:else if col.type === "date"}
  <DateInput {id} bind:value={value as string | null} />
{:else if col.type === "int4range" && col.format === "tod"}
  <div class="row-inputs" {id}>
    <TimeInput aria-label={t("ui.from")} bind:value={() => range[0], (v) => setRange(0, v)} />
    <TimeInput aria-label={t("ui.to")} bind:value={() => range[1], (v) => setRange(1, v)} />
  </div>
{:else if col.type.startsWith("_") && col.format === "min"}
  <div class="row-inputs" {id}>
    {#each [...list, null] as v, i (i)}
      <DurationInput bind:value={() => v, (x) => setAt(i, x)} />
    {/each}
  </div>
{:else if col.format === "min"}
  <DurationInput {id} bind:value={value as number | null} />
{:else if col.format === "tod"}
  <TimeInput {id} bind:value={value as number | null} />
{:else if isNumeric(col)}
  <NumberInput {id} bind:value={value as number | null} money={col.format === "money"} />
{:else}
  <Input {id} bind:value={() => (value as string) ?? "", (v) => (value = v === "" && col.nullable ? null : v)} />
{/if}

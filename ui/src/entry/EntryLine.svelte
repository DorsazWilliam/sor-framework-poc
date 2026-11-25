<script lang="ts">
  import type { Line } from "./store.svelte";
  import { allowedTasks, billing, folderItems, taskLabel } from "./store.svelte";
  import Combobox from "../ds/Combobox.svelte";
  import DurationInput from "../ds/DurationInput.svelte";
  import Input from "../ds/Input.svelte";
  import TimeInput from "../ds/TimeInput.svelte";
  import Check from "~icons/fluent/checkmark-16-regular";
  import Refused from "~icons/fluent/error-circle-16-filled";
  import Flag from "~icons/fluent/flag-16-regular";
  import Validated from "~icons/fluent/checkmark-circle-16-regular";
  import Invoiced from "~icons/fluent/receipt-16-regular";
  import Locked from "~icons/fluent/lock-closed-16-regular";
  import { lookup } from "../app/lookups.svelte";
  import { command } from "../app/session.svelte";
  import { evaluate } from "../screen/commands";
  import { fmtDuration, fmtMoney, fmtTime } from "../ds/format";
  import { t } from "../labels/i18n.svelte";

  // One activity line: inputs when the row is editable, text otherwise (the row itself takes focus).
  let {
    line,
    start,
    active,
    onleave,
  }: { line: Line; start: number; active: boolean; onleave: (l: Line) => void } = $props();

  const r = $derived(line.row ?? {});
  const edit = $derived(line.row ? evaluate(command("activity.update")!, [line.row]) : evaluate(command("activity.create")!, []));
  const d = $derived(line.draft);
  const end = $derived(d.duration_min == null ? "" : fmtTime(Math.min(1440, (d.start_tod ?? start) + d.duration_min)));
  const sometimes = $derived(billing(d.task_id) === "sometimes");
  const folder = $derived(d.folder_id == null ? "" : (lookup("folder", d.folder_id) ?? ""));
  const states = $derived(
    (
      [
        [!!r.to_validate && r.not_validated !== false, Flag, "warning", "to_validate"],
        [r.is_validated === true, Validated, "positive", "validated"],
        [r.not_invoiced === false, Invoiced, "accent", "invoiced"],
        [r.not_locked === false, Locked, "muted", "locked"],
      ] as const
    ).filter(([on]) => on),
  );
  const num = (v: unknown, f: (n: number) => string) => (v == null ? "" : f(v as number));
</script>

<div
  class="line"
  class:ro={!edit.enabled}
  class:refused={line.status === "refused"}
  class:fresh={line.fresh}
  data-uid={line.uid}
  data-row={line.id ?? "new"}
  data-saved={line.saved}
  tabindex={edit.enabled ? undefined : 0}
  role="row"
  title={edit.enabled ? undefined : t("ui.read_only", { reasons: edit.reasons.join(", ") })}
  onfocusout={(e) => !e.currentTarget.contains(e.relatedTarget as Node | null) && onleave(line)}
>
  <div class="c ind" title={line.status === "refused" ? t("ui.entry.refused", { list: line.reasons.join(", ") }) : line.status ? t(`ui.entry.${line.status}`) : undefined}>
    {#if line.status === "saving"}
      <span class="dot"></span>
    {:else if line.status === "saved"}
      {#key line.saved}<span class="ok"><Check /></span>{/key}
    {:else if line.status === "refused"}
      <span class="ko"><Refused /></span>
    {/if}
  </div>
  {#key line.rev}
    {#if edit.enabled}
      <div class="c" data-col="start_tod"><TimeInput bind:value={line.draft.start_tod} placeholder={fmtTime(start)} aria-label={t("c.start_tod")} /></div>
      <div class="c" data-col="duration_min"><DurationInput bind:value={line.draft.duration_min} aria-label={t("c.duration_min")} /></div>
      <div class="c num end">{end}</div>
      <div class="c" data-col="folder_id">
        <Combobox
          items={folderItems()}
          value={d.folder_id == null ? null : String(d.folder_id)}
          label={folder}
          onValueChange={(v) => (line.draft.folder_id = v == null ? null : +v)}
        />
      </div>
      <div class="c" data-col="task_id">
        <Combobox
          items={active ? allowedTasks(d.folder_id) : []}
          value={d.task_id == null ? null : String(d.task_id)}
          label={taskLabel(d.task_id)}
          onValueChange={(v) => (line.draft.task_id = v == null ? null : +v)}
        />
      </div>
      {#if sometimes}
        <div class="c" data-col="billed_min">
          <DurationInput bind:value={line.draft.billed_min} placeholder={num(d.duration_min, fmtDuration)} aria-label={t("c.billed_min")} />
        </div>
      {:else}
        <div class="c num end">{num(r.billed_min, fmtDuration)}</div>
      {/if}
      <div class="c" data-col="description">
        <Input bind:value={() => d.description ?? "", (v) => (line.draft.description = v || null)} aria-label={t("c.description")} />
      </div>
    {:else}
      <div class="c num text">{num(d.start_tod, fmtTime)}</div>
      <div class="c num text">{num(d.duration_min, fmtDuration)}</div>
      <div class="c num end">{end}</div>
      <div class="c text" title={folder}>{folder}</div>
      <div class="c text" title={taskLabel(d.task_id)}>{taskLabel(d.task_id)}</div>
      <div class="c num end">{num(r.billed_min, fmtDuration)}</div>
      <div class="c text" title={d.description}>{d.description ?? ""}</div>
    {/if}
  {/key}
  <div class="c states">
    {#each states as [, I, kind, key] (key)}<span class="st" data-kind={kind} title={t(`ui.entry.${key}`)}><I /></span>{/each}
  </div>
  <div class="c num text">{num(r.amount, fmtMoney)}</div>
</div>
{#if line.status === "refused" && line.reasons.length}
  <div class="reasons" role="alert"><Refused />{t("ui.entry.refused", { list: line.reasons.join(", ") })}</div>
{/if}

<style>
  .line {
    display: grid;
    grid-template-columns: var(--cols);
    height: var(--h-control);
    border-bottom: var(--bw) solid var(--border);
    outline: none;
  }
  .line:hover {
    background: var(--hover);
  }
  .line:focus-within,
  .line:focus-visible {
    background: var(--selection);
  }
  .line.refused {
    box-shadow: inset 3px 0 0 var(--negative);
  }
  .line.ro {
    color: var(--text-muted);
  }
  .c {
    display: flex;
    align-items: center;
    min-width: 0;
  }
  .c.num {
    justify-content: flex-end;
  }
  .text,
  .end {
    display: block;
    padding: var(--cell-pad);
    overflow: hidden;
    line-height: calc(var(--h-control) - 1px);
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .text.num,
  .end.num {
    text-align: right;
  }
  .end {
    color: var(--text-muted);
  }
  .c > :global(.input),
  .c > :global(* > .input) {
    height: calc(var(--h-control) - 1px);
    padding: 0 var(--s-4);
    border-color: transparent;
    border-radius: 0;
    background: transparent;
    font-size: var(--fs-grid);
  }
  .c :global(.input:hover) {
    border-color: var(--border);
  }
  .c :global(.input:focus),
  .c :global(.input:focus-within) {
    border-color: var(--border);
    border-bottom: 2px solid var(--accent);
    background: var(--surface);
  }
  .c :global(.combo) {
    width: 100%;
    padding-right: 0;
  }
  .c :global(.field-btn) {
    width: 20px;
    visibility: hidden;
  }
  .line:hover :global(.field-btn),
  .c:focus-within :global(.field-btn) {
    visibility: visible;
  }
  .fresh :global(input::placeholder) {
    color: var(--text-disabled);
  }
  .ind {
    justify-content: center;
  }
  .dot {
    width: 6px;
    height: 6px;
    border-radius: 3px;
    background: var(--accent);
    animation: pulse 0.8s ease-in-out infinite alternate;
  }
  .ok {
    display: flex;
    color: var(--positive);
    animation: fade 2s ease-in forwards;
  }
  .ko {
    display: flex;
    color: var(--negative);
  }
  .states {
    gap: var(--s-2);
    padding: 0 var(--s-4);
  }
  .st {
    display: flex;
  }
  .st[data-kind="warning"] {
    color: var(--warning);
  }
  .st[data-kind="positive"] {
    color: var(--positive);
  }
  .st[data-kind="accent"] {
    color: var(--accent);
  }
  .st[data-kind="muted"] {
    color: var(--text-muted);
  }
  .reasons {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    height: 22px;
    padding-left: calc(22px + var(--s-4));
    border-bottom: var(--bw) solid var(--border);
    background: var(--negative-subtle);
    color: var(--negative);
    font-size: var(--fs-caption);
  }
  .reasons :global(svg) {
    width: 12px;
    height: 12px;
  }
  @keyframes pulse {
    to {
      opacity: 0.3;
    }
  }
  @keyframes fade {
    70% {
      opacity: 1;
    }
    to {
      opacity: 0;
    }
  }
</style>

<script lang="ts">
  import type { Command, Rejected, Row } from "../api/contract";
  import Dialog from "../ds/Dialog.svelte";
  import Button from "../ds/Button.svelte";
  import Field from "../ds/Field.svelte";
  import Badge from "../ds/Badge.svelte";
  import FieldEditor from "./FieldEditor.svelte";
  import { execute } from "./commands";
  import { ApiError } from "../api/client";
  import { follow } from "../app/cache.svelte";
  import { colLabel, command } from "../app/session.svelte";
  import { t } from "../labels/i18n.svelte";

  let { cmd, initial, onclose }: { cmd: Command; initial: Record<string, unknown>; onclose: () => void } = $props();

  let open = $state(true);
  const prefill = () => Object.fromEntries(cmd.input.filter((c) => initial[c.name] !== undefined).map((c) => [c.name, initial[c.name]]));
  const values = $state<Record<string, unknown>>(prefill());
  let runId = $state<number | null>(null);
  let run = $state<Row | null>(null);
  const finished = $derived(run != null && ["done", "cancelled", "failed"].includes(run.state as string));
  const rejected = $derived((run?.rejected as Rejected[] | null) ?? []);

  $effect(() => {
    if (runId == null) return;
    return follow([["run", { filter: [{ col: "id", op: "eq", value: runId }] }]], ([rows]) => (run = rows[0] ?? null));
  });

  $effect(() => {
    if (!open) onclose();
  });

  async function start(e: Event) {
    e.preventDefault();
    const res = await execute(cmd, { ...values });
    if (!(res instanceof ApiError)) runId = res.run ?? null;
  }

  const cancel = () => execute(command("run.cancel")!, { ids: [runId] });
</script>

<Dialog bind:open title={t(`cmd.${cmd.name}`)}>
  {#if runId == null}
    <form id="run-form" onsubmit={start}>
      {#each cmd.input as c (c.name)}
        <Field label={colLabel(c.name)} id="run-{c.name}">
          <FieldEditor col={c} id="run-{c.name}" bind:value={values[c.name]} />
        </Field>
      {/each}
    </form>
  {:else}
    <div class="run" data-run-state={run?.state}>
      <div class="line">
        <Badge kind={run?.state === "done" ? "positive" : run?.state === "running" ? "accent" : run?.state === "queued" ? "neutral" : "warning"}>
          {t(`e.run_state.${run?.state ?? "queued"}`)}
        </Badge>
        <span class="num">{run?.done ?? 0} / {run?.total ?? "…"}</span>
      </div>
      <div class="progress" role="progressbar" aria-valuemin={0} aria-valuemax={(run?.total as number) ?? 0} aria-valuenow={(run?.done as number) ?? 0}>
        <div style:width="{run?.total ? (100 * (run.done as number)) / (run.total as number) : 0}%"></div>
      </div>
      {#if run?.error}<p class="neg">{run.error}</p>{/if}
      {#if rejected.length}
        <p class="caption">{t("ui.rejected_roots", { n: rejected.length })}</p>
        <ul class="rejected">
          {#each rejected as r (r.id)}<li>#{r.id} — {r.conditions.map((k) => t(`cond.${k}`)).join(", ")}</li>{/each}
        </ul>
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    {#if runId == null}
      <Button onclick={() => (open = false)}>{t("ui.cancel")}</Button>
      <Button variant="primary" type="submit" form="run-form">{t("ui.start")}</Button>
    {:else if finished}
      <Button variant="primary" onclick={() => (open = false)}>{t("ui.close")}</Button>
    {:else}
      <Button onclick={cancel} disabled={!!run?.cancel_requested}>{t("cmd.run.cancel")}</Button>
    {/if}
  {/snippet}
</Dialog>

<style>
  .run {
    display: flex;
    flex-direction: column;
    gap: var(--s-4);
  }
  .line {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .rejected {
    max-height: 160px;
    margin: 0;
    padding-left: var(--s-6);
    overflow: auto;
  }
</style>

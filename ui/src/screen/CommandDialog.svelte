<script lang="ts">
  import type { Command, Rejected } from "../api/contract";
  import Dialog from "../ds/Dialog.svelte";
  import Button from "../ds/Button.svelte";
  import Field from "../ds/Field.svelte";
  import FieldEditor from "./FieldEditor.svelte";
  import { execute } from "./commands";
  import { colLabel } from "../app/session.svelte";
  import { t } from "../labels/i18n.svelte";
  import { ApiError } from "../api/client";

  let {
    cmd,
    ids,
    onclose,
    onRejected,
  }: { cmd: Command; ids: number[]; onclose: () => void; onRejected: (r: Rejected[]) => void } = $props();

  let open = $state(true);
  let busy = $state(false);
  const values = $state<Record<string, unknown>>({});

  async function submit(e: Event) {
    e.preventDefault();
    busy = true;
    const res = await execute(cmd, { ...(cmd.ids ? { ids } : {}), ...values }, onRejected);
    busy = false;
    if (!(res instanceof ApiError)) open = false;
  }

  $effect(() => {
    if (!open) onclose();
  });
</script>

<Dialog bind:open title={t(`cmd.${cmd.name}`)}>
  <form id="cmd-form" onsubmit={submit}>
    {#if cmd.ids}<p class="muted">{t("ui.applies_to", { n: ids.length })}</p>{/if}
    {#each cmd.input as c (c.name)}
      <Field label={colLabel(c.name, cmd.table)} id="cmd-{c.name}">
        <FieldEditor col={c} id="cmd-{c.name}" bind:value={values[c.name]} />
      </Field>
    {/each}
  </form>
  {#snippet footer()}
    <Button onclick={() => (open = false)}>{t("ui.cancel")}</Button>
    <Button variant="primary" type="submit" form="cmd-form" disabled={busy}>{t(cmd.kind === "delete" ? "ui.delete" : "ui.run")}</Button>
  {/snippet}
</Dialog>

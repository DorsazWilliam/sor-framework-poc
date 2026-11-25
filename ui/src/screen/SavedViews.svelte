<script lang="ts">
  import type { Row } from "../api/contract";
  import type { ScreenState } from "../app/router.svelte";
  import Menu, { type MenuItem } from "../ds/Menu.svelte";
  import Button from "../ds/Button.svelte";
  import Dialog from "../ds/Dialog.svelte";
  import Field from "../ds/Field.svelte";
  import Input from "../ds/Input.svelte";
  import Bookmark from "~icons/fluent/bookmark-16-regular";
  import Chevron from "~icons/fluent/chevron-down-16-regular";
  import { api, ApiError } from "../api/client";
  import { watch } from "../app/live.svelte";
  import { command, session, view } from "../app/session.svelte";
  import { execute } from "./commands";
  import { t } from "../labels/i18n.svelte";

  // Saved views are rows of the kernel table saved_view (owner, screen, name, state).
  let { screen, current, apply }: { screen: string; current: () => ScreenState; apply: (s: ScreenState) => void } =
    $props();

  let saved = $state<Row[]>([]);
  let active = $state<Row | null>(null);
  let naming = $state(false);
  let name = $state("");

  async function load() {
    saved = await api.view("saved_view", {
      filter: [{ col: "screen", op: "eq", value: screen }],
      sort: [{ col: "name", dir: "asc" }],
    });
    if (active) active = saved.find((s) => s.id === active!.id) ?? null;
  }

  $effect(() => {
    load();
    return watch(view("saved_view")?.roots ?? ["saved_view"], load);
  });

  async function create(e: Event) {
    e.preventDefault();
    const res = await execute(command("saved_view.create")!, { owner_id: session.me.self, screen, name, state: current() });
    if (res instanceof ApiError) return;
    naming = false;
    await load();
    active = saved.find((s) => s.id === res.ids[0]) ?? null;
  }

  const items = $derived<MenuItem[]>([
    ...(saved.length ? [{ heading: t("ui.saved_views") }] : []),
    ...saved.map((s) => ({
      label: s.name as string,
      checked: active?.id === s.id,
      onselect: () => {
        active = s;
        apply(s.state as ScreenState);
      },
    })),
    ...(saved.length ? ["separator" as const] : []),
    {
      label: t("ui.save_view"),
      disabled: !active,
      onselect: () => execute(command("saved_view.update")!, { ids: [active!.id], state: current() }),
    },
    {
      label: t("ui.save_view_as"),
      onselect: () => {
        name = "";
        naming = true;
      },
    },
    {
      label: t("ui.delete_view"),
      disabled: !active,
      onselect: async () => {
        await execute(command("saved_view.delete")!, { ids: [active!.id] });
        active = null;
      },
    },
  ]);
</script>

<Menu {items}>
  {#snippet trigger(props)}
    <Button variant="subtle" icon={Bookmark} {...props}>
      {(active?.name as string) ?? t("ui.saved_views")}
      <Chevron />
    </Button>
  {/snippet}
</Menu>

{#if naming}
  <Dialog bind:open={naming} title={t("ui.save_view_as")}>
    <form id="sv-form" onsubmit={create}>
      <Field label={t("ui.name")} id="sv-name"><Input id="sv-name" bind:value={name} required /></Field>
    </form>
    {#snippet footer()}
      <Button onclick={() => (naming = false)}>{t("ui.cancel")}</Button>
      <Button variant="primary" type="submit" form="sv-form" disabled={!name.trim()}>{t("ui.save")}</Button>
    {/snippet}
  </Dialog>
{/if}

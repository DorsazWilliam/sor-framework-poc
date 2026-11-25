<script lang="ts">
  import Toggle from "~icons/fluent/navigation-20-regular";
  import Other from "~icons/fluent/text-bullet-list-square-20-regular";
  import Open from "~icons/fluent/chevron-down-12-regular";
  import Closed from "~icons/fluent/chevron-right-12-regular";
  import Dot from "~icons/fluent/circle-12-filled";
  import ViewScreen from "../screen/ViewScreen.svelte";
  import Entry from "../entry/Entry.svelte";
  import Toaster from "../ds/Toaster.svelte";
  import { groupIcons, screenIcons } from "./icons";
  import { screenLabel, screenView, session, store, stored } from "../app/session.svelte";
  import { live } from "../app/live.svelte";
  import { go, route } from "../app/router.svelte";
  import { t, tn } from "../labels/i18n.svelte";

  let collapsed = $state(false);

  const allowed = session.meta.screens.filter((s) => screenView(s));
  const groups = session.meta.groups
    .map((g) => ({ g, screens: allowed.filter((s) => s.group === g) }))
    .filter((x) => x.screens.length);
  const screen = $derived(allowed.find((s) => s.id === route.screen));

  // Closed groups are remembered; by default long groups start closed. The current screen's group stays open.
  let closed = $state<string[]>(JSON.parse(stored("nav.closed") ?? "null") ?? groups.filter((x) => x.screens.length > 6).map((x) => x.g));
  const isOpen = (g: string) => !closed.includes(g) || screen?.group === g;
  function toggle(g: string) {
    if (collapsed) collapsed = false;
    else closed = isOpen(g) ? [...closed, g] : closed.filter((x) => x !== g);
    store("nav.closed", JSON.stringify(closed));
  }

  $effect(() => {
    if (!screen && allowed.length) go(allowed[0].id);
  });
</script>

<div class="shell" class:collapsed>
  <nav class="rail" aria-label={t("ui.navigation")}>
    <button class="toggle" aria-label={t("ui.toggle_nav")} aria-expanded={!collapsed} onclick={() => (collapsed = !collapsed)}>
      <Toggle />
    </button>
    {#each groups as { g, screens } (g)}
      {@const I = groupIcons[g] ?? Other}
      <div class="group">
        <button class="ghead" title={t(`g.${g}`)} aria-expanded={!collapsed && isOpen(g)} onclick={() => toggle(g)}>
          <I /><span>{t(`g.${g}`)}</span>
          {#if !collapsed}<span class="chev">{#if isOpen(g)}<Open />{:else}<Closed />{/if}</span>{/if}
        </button>
        {#if !collapsed && isOpen(g)}
          {#each screens as s (s.id)}
            {@const SI = screenIcons[s.id] ?? I}
            <a class="item" href="#/{s.id}" aria-current={s.id === route.screen ? "page" : undefined}><SI /><span>{screenLabel(s)}</span></a>
          {/each}
        {/if}
      </div>
    {/each}
  </nav>

  <main>
    {#if screen}
      {#key `${screen.id}:${route.visit}`}
        {#if screen.component === "entry"}<Entry {screen} />{:else}<ViewScreen {screen} />{/if}
      {/key}
    {/if}
  </main>

  <footer class="status">
    <span>{tn("ui.rows", session.status.rows)}</span>
    {#if session.status.selected}<span>{tn("ui.selected", session.status.selected)}</span>{/if}
    <span class="live" data-state={live.state}><Dot />{t(`ui.live.${live.state}`)}</span>
  </footer>
</div>
<Toaster />

<style>
  .shell {
    display: grid;
    grid-template: "rail main" minmax(0, 1fr) "rail status" var(--h-status) / var(--w-nav) minmax(0, 1fr);
    height: 100%;
  }
  .shell.collapsed {
    grid-template-columns: var(--w-nav-collapsed) minmax(0, 1fr);
  }
  .rail {
    grid-area: rail;
    display: flex;
    flex-direction: column;
    gap: var(--s-2);
    padding: var(--s-2);
    overflow-x: hidden;
    overflow-y: auto;
    border-right: var(--bw) solid var(--border);
    background: var(--bg-app);
  }
  .rail :global(svg) {
    width: var(--icon-nav);
    height: var(--icon-nav);
  }
  .toggle,
  .ghead,
  .item {
    display: flex;
    align-items: center;
    gap: var(--s-5);
    height: var(--h-nav);
    padding: 0 var(--s-4) 0 var(--s-3);
    border: 0;
    border-radius: var(--r-button);
    background: transparent;
    color: var(--text);
    text-decoration: none;
    white-space: nowrap;
    cursor: pointer;
  }
  .toggle {
    width: 36px;
    padding: 0 var(--s-3);
  }
  .group + .group {
    margin-top: var(--s-3);
  }
  .ghead {
    width: 100%;
    color: var(--text-muted);
    font-size: var(--fs-grid);
    font-weight: var(--fw-semibold);
  }
  .ghead .chev {
    display: flex;
    margin-left: auto;
  }
  .ghead .chev :global(svg) {
    width: 12px;
    height: 12px;
  }
  .collapsed .ghead span {
    display: none;
  }
  .item {
    position: relative;
    gap: var(--s-4);
    padding-left: 34px;
  }
  .item :global(svg) {
    width: var(--icon);
    height: var(--icon);
    color: var(--text-muted);
  }
  .toggle:hover,
  .item:hover,
  .ghead:hover {
    background: var(--hover);
  }
  .item[aria-current="page"] {
    background: var(--surface);
    font-weight: var(--fw-semibold);
  }
  .item[aria-current="page"] :global(svg) {
    color: var(--accent);
  }
  .item[aria-current="page"]::before {
    content: "";
    position: absolute;
    left: 0;
    top: 8px;
    bottom: 8px;
    width: 3px;
    border-radius: 2px;
    background: var(--accent);
  }
  main {
    grid-area: main;
    display: flex;
    min-width: 0;
    min-height: 0;
  }
  .status {
    grid-area: status;
    display: flex;
    align-items: center;
    gap: var(--s-7);
    padding: 0 var(--s-6);
    border-top: var(--bw) solid var(--border);
    background: var(--bg-app);
    font-size: var(--fs-caption);
    color: var(--text-muted);
    font-variant-numeric: tabular-nums;
  }
  .live {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    margin-left: auto;
  }
  .live :global(svg) {
    width: 8px;
    height: 8px;
    color: var(--warning);
  }
  .live[data-state="live"] :global(svg) {
    color: var(--positive);
  }
  .live[data-state="offline"] :global(svg) {
    color: var(--negative);
  }
</style>

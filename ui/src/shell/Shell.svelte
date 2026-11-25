<script lang="ts">
  import Toggle from "~icons/fluent/navigation-20-regular";
  import Time from "~icons/fluent/clock-20-regular";
  import Billing from "~icons/fluent/receipt-20-regular";
  import Analysis from "~icons/fluent/data-bar-vertical-20-regular";
  import MasterData from "~icons/fluent/database-20-regular";
  import System from "~icons/fluent/settings-20-regular";
  import Other from "~icons/fluent/text-bullet-list-square-20-regular";
  import Dot from "~icons/fluent/circle-12-filled";
  import ViewScreen from "../screen/ViewScreen.svelte";
  import Entry from "../entry/Entry.svelte";
  import Toaster from "../ds/Toaster.svelte";
  import type { Icon } from "../ds/types";
  import { screenView, session } from "../app/session.svelte";
  import { live } from "../app/live.svelte";
  import { go, route } from "../app/router.svelte";
  import { fmtNumber } from "../ds/format";
  import { t } from "../labels/i18n.svelte";

  const groupIcons: Record<string, Icon> = { time: Time, billing: Billing, analysis: Analysis, master_data: MasterData, system: System };

  let collapsed = $state(false);

  const allowed = session.meta.screens.filter((s) => screenView(s));
  const groups = session.meta.groups
    .map((g) => ({ g, screens: allowed.filter((s) => s.group === g) }))
    .filter((x) => x.screens.length);
  const screen = $derived(allowed.find((s) => s.id === route.screen));

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
        <button class="ghead" tabindex={collapsed ? 0 : -1} title={t(`g.${g}`)} onclick={() => (collapsed = false)}>
          <I /><span>{t(`g.${g}`)}</span>
        </button>
        {#if !collapsed}
          {#each screens as s (s.id)}
            <a class="item" href="#/{s.id}" aria-current={s.id === route.screen ? "page" : undefined}>{t(`s.${s.id}`)}</a>
          {/each}
        {/if}
      </div>
    {/each}
  </nav>

  <main>
    {#if screen}
      {#key screen.id}
        {#if screen.component === "entry"}<Entry {screen} />{:else}<ViewScreen {screen} />{/if}
      {/key}
    {/if}
  </main>

  <footer class="status">
    <span>{t("ui.rows", { n: fmtNumber(session.status.rows) })}</span>
    {#if session.status.selected}<span>{t("ui.selected", { n: fmtNumber(session.status.selected) })}</span>{/if}
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
  .ghead {
    font-weight: var(--fw-semibold);
    cursor: default;
  }
  .collapsed .ghead {
    cursor: pointer;
  }
  .collapsed .ghead span {
    display: none;
  }
  .item {
    position: relative;
    padding-left: 40px;
  }
  .toggle:hover,
  .item:hover,
  .collapsed .ghead:hover {
    background: var(--hover);
  }
  .item[aria-current="page"] {
    background: var(--surface);
    font-weight: var(--fw-semibold);
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

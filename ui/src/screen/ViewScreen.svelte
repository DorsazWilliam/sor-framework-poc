<script lang="ts">
  import type { Command, Filter, Row, Screen, Sort } from "../api/contract";
  import CommandBar from "../ds/CommandBar.svelte";
  import Button from "../ds/Button.svelte";
  import Tooltip from "../ds/Tooltip.svelte";
  import Select from "../ds/Select.svelte";
  import Drawer from "../ds/Drawer.svelte";
  import Dialog from "../ds/Dialog.svelte";
  import Grid from "../grid/Grid.svelte";
  import { Source } from "../grid/source.svelte";
  import FieldEditor from "./FieldEditor.svelte";
  import Detail from "./Detail.svelte";
  import CommandDialog from "./CommandDialog.svelte";
  import RunDialog from "./RunDialog.svelte";
  import SavedViews from "./SavedViews.svelte";
  import UserMenu from "../shell/UserMenu.svelte";
  import FilterOff from "~icons/fluent/dismiss-circle-16-filled";
  import { evaluate, execute, kindIcon, takesIds } from "./commands";
  import { colLabel, command, screenLabel, screenView, session, table } from "../app/session.svelte";
  import { isNumeric } from "../app/display";
  import { lookup } from "../app/lookups.svelte";
  import { watch } from "../app/live.svelte";
  import { defaults, linkWrap } from "../app/params";
  import { keep, route, type ScreenState } from "../app/router.svelte";
  import { t } from "../labels/i18n.svelte";

  // Renders any Screen of the meta: params, grid on its first permitted view, commands, detail drawer.
  // A child screen (in a drawer tab) keeps no URL state; its `link` values fill params or filter the view.
  let { screen, link, parent }: { screen: Screen; link?: Record<string, unknown>; parent?: Row } = $props();

  const nested = $derived(link !== undefined);
  const v = $derived(screenView(screen));
  const tbl = $derived(screen.table ? table(screen.table) : undefined);
  // Through its JSON: a parent re-render with the same values does not reset the grid.
  const linkJson = $derived(JSON.stringify(link ?? {}));
  const linked = $derived(linkWrap(v, JSON.parse(linkJson)));

  // Full state from a partial one (URL or saved view; ignored for a child screen) over the screen's defaults and wrap.
  const merge = (given: ScreenState) => {
    const s = link ? {} : given;
    return {
    params: { ...defaults(screen.defaults, session.me), ...s.params, ...linked.params },
    filter: s.wrap?.filter ?? screen.wrap?.filter ?? [],
    sort: s.wrap?.sort ?? screen.wrap?.sort ?? [],
    group: s.wrap?.group ?? screen.wrap?.group ?? [],
    widths: s.widths ?? {},
    };
  };
  const init = merge(route.state);

  let params = $state<Record<string, unknown>>(init.params);
  let filter = $state<Filter[]>(init.filter);
  let sort = $state<Sort[]>(init.sort);
  let group = $state<string[]>(init.group);
  let widths = $state<Record<string, number>>(init.widths);
  let selection = $state.raw(new Map<number, Row>());
  let flags = $state.raw(new Map<number, string[]>());
  let ready = $state(false);
  let detail = $state<{ id: number | null } | null>(null);
  let dialog = $state<Command | null>(null);
  let drawerWidth = $state(420);

  const source = $derived(
    new Source({
      view: v.name,
      ...$state.snapshot({ params, sort, group }),
      filter: [...linked.filter, ...$state.snapshot(filter)],
      agg: screen.wrap?.agg ?? [],
      count: v.columns[0].name,
    }),
  );
  const rows = $derived([...selection.values()].map((r) => source.byId(r.id) ?? r));
  const ids = $derived(rows.map((r) => r.id as number));
  const cmds = $derived(
    [...new Set([...(tbl ? ["create", "update", "delete"].map((k) => `${tbl.name}.${k}`) : []), ...screen.commands])]
      .map(command)
      .filter((c): c is Command => !!c),
  );
  // `id` and the conditions of the view's root tables stay in the rows (commands read them) but are not displayed.
  const conditions = $derived(new Set(v.roots.flatMap((r) => table(r)?.conditions ?? [])));
  const columns = $derived(v.columns.filter((c) => c.name !== "id" && !conditions.has(c.name)));
  const shownParams =$derived(v.params.filter((p) => !(link && p.name in link)));
  const groupItems = $derived([
    { value: "", label: t("ui.no_grouping") },
    ...columns.filter((c) => !isNumeric(c) && c.type !== "bool").map((c) => ({ value: c.name, label: colLabel(c.name, tbl?.name) })),
  ]);
  const initial = $derived(
    tbl ? Object.fromEntries(tbl.columns.flatMap((c) => {
      const v = link?.[c.name] ?? params[c.name];
      return v === undefined ? [] : [[c.name, v]];
    })) : {},
  );

  // The grid keeps showing the previous result until the new query has its first answer.
  let shown = $state.raw<Source>();
  $effect(() => {
    if (source.ready || !shown) shown = source;
  });

  $effect(() => {
    void source;
    selection = new Map();
    flags = new Map();
  });

  $effect(() => {
    const s = source;
    return watch(v.roots, () => s.refresh());
  });

  $effect(() => {
    if (nested) return;
    session.status = { rows: (shown?.totals?.count as number) ?? 0, selected: selection.size };
    keep($state.snapshot({ params, wrap: { filter, sort, group }, widths }));
  });

  $effect(() => {
    if (detail?.id != null && rows.length === 1 && rows[0].id !== detail.id) detail = { id: rows[0].id as number };
  });

  const current = (): ScreenState => $state.snapshot({ params, wrap: { filter, sort, group }, widths });
  function apply(s: ScreenState) {
    ({ params, filter, sort, group, widths } = merge(s));
  }

  function flag(r: { id: number; conditions: string[] }[]) {
    flags = new Map(r.map((x) => [x.id, x.conditions.map((k) => t(`cond.${k}`))]));
  }

  function invoke(c: Command) {
    flags = new Map();
    if (c.kind === "create") detail = { id: null };
    else if (c.kind === "update") detail = { id: ids[0] };
    else if (c.kind === "custom" && !c.rootless && !c.input.length) execute(c, { ids }, flag);
    else dialog = c;
  }

  function open(r: Row) {
    if (tbl && r.id != null) detail = { id: r.id as number };
  }

  // The bar names the screen's table: its CRUD commands show the verb only (full name in the title).
  const crud = new Set(["create", "update", "delete"]);
</script>

{#snippet commandButtons()}
  {#each cmds as c (c.name)}
    {@const s = evaluate(c, rows, parent)}
    <Tooltip text={s.enabled ? "" : s.reasons.join(" · ")}>
      <Button variant="subtle" icon={kindIcon[c.kind]} disabled={!s.enabled} data-command={c.name} title={s.enabled ? t(`cmd.${c.name}`) : undefined} onclick={() => invoke(c)}>
        {t(crud.has(c.kind) && c.table === tbl?.name ? `cmd.${c.kind}` : `cmd.${c.name}`)}
      </Button>
    </Tooltip>
  {/each}
{/snippet}

<section class="screen" class:nested data-screen={screen.id} data-ready={ready || undefined}>
  {#if nested}
    <div class="subbar">{@render commandButtons()}</div>
  {:else}
    <CommandBar title={screenLabel(screen)}>
      {@render commandButtons()}
      {#snippet end()}<UserMenu />{/snippet}
    </CommandBar>
  {/if}

  {#if !nested || shownParams.length || filter.length}
    <div class="params">
      {#each shownParams as p (p.name)}
        <label class="param" class:wide={!!p.ref}>
          <span>{colLabel(p.name)}</span>
          <FieldEditor col={p} id="p-{screen.id}-{p.name}" bind:value={params[p.name]} />
        </label>
      {/each}
      {#if !nested}
        <label class="param wide">
          <span>{t("ui.group_by")}</span>
          <Select id="group-{screen.id}" items={groupItems} bind:value={() => group[0] ?? "", (g) => (group = g ? [g] : [])} />
        </label>
      {/if}
      <div class="spacer"></div>
      {#if filter.length}
        <Button variant="subtle" icon={FilterOff} onclick={() => (filter = [])}>{t("ui.clear_filters")}</Button>
      {/if}
      {#if !nested}<SavedViews screen={screen.id} {current} {apply} />{/if}
    </div>
  {/if}

  <div class="content">
    <Grid
      source={shown ?? source}
      {columns}
      label={(c) => colLabel(c.name, tbl?.name)}
      bind:widths
      bind:sort
      bind:filter
      bind:selection
      bind:ready
      {flags}
      onopen={open}
    />
    {#if detail && tbl && !nested}
      <Drawer
        title={detail.id == null ? t("ui.new_row", { table: t(`t.${tbl.name}`) }) : (lookup(tbl.name, detail.id) ?? `#${detail.id}`)}
        bind:width={drawerWidth}
        onclose={() => (detail = null)}
      >
        <Detail {screen} {tbl} id={detail.id} {initial} onsaved={(id) => (detail = { id })} />
      </Drawer>
    {/if}
  </div>
</section>

{#if detail && tbl && nested}
  <Dialog open title={t(`t.${tbl.name}`)}>
    <Detail {screen} {tbl} id={detail.id} {initial} tabs={false} {parent} onsaved={() => (detail = null)} />
  </Dialog>
{/if}

{#if dialog?.kind === "run"}
  <RunDialog cmd={dialog} initial={params} onclose={() => (dialog = null)} />
{:else if dialog}
  <CommandDialog cmd={dialog} ids={takesIds(dialog) ? ids : []} onclose={() => (dialog = null)} onRejected={flag} />
{/if}

<style>
  .screen {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .subbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--s-1);
    padding: var(--s-2) var(--s-2);
    border-bottom: var(--bw) solid var(--border);
  }
  .params {
    display: flex;
    align-items: center;
    gap: var(--s-5);
    min-height: var(--h-params);
    padding: var(--s-1) var(--s-4) var(--s-1) var(--s-6);
    border-bottom: var(--bw) solid var(--border);
    background: var(--surface);
  }
  .nested .params {
    padding-left: var(--s-4);
  }
  .param {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    width: 210px;
    white-space: nowrap;
  }
  .param.wide {
    width: 250px;
  }
  .param > span {
    color: var(--text-muted);
  }
  .spacer {
    flex: 1;
  }
  .content {
    display: flex;
    flex: 1;
    min-height: 0;
  }
</style>

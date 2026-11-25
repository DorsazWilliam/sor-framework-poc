<script lang="ts">
  import { untrack, type Snippet } from "svelte";
  import type { Col, ColFilter, Filter, Row, Sort } from "../api/contract";
  import { GROUP, ROW, type Source } from "./source.svelte";
  import GridFilter from "./GridFilter.svelte";
  import Badge from "../ds/Badge.svelte";
  import Tooltip from "../ds/Tooltip.svelte";
  import Up from "~icons/fluent/arrow-sort-up-16-regular";
  import Down from "~icons/fluent/arrow-sort-down-16-regular";
  import Open from "~icons/fluent/chevron-down-16-regular";
  import Closed from "~icons/fluent/chevron-right-16-regular";
  import Check from "~icons/fluent/checkmark-12-regular";
  import Dash from "~icons/fluent/subtract-12-regular";
  import Attach from "~icons/fluent/attach-16-regular";
  import { defaultWidth, display, isNumeric } from "../app/display";
  import { fmtNumber } from "../ds/format";
  import { t } from "../labels/i18n.svelte";

  let {
    source,
    columns,
    label,
    widths = $bindable({}),
    sort = $bindable([]),
    filter = $bindable([]),
    selection = $bindable(new Map()),
    ready = $bindable(false),
    flags,
    onopen,
    cell,
  }: {
    source: Source;
    columns: Col[];
    label: (c: Col) => string;
    widths?: Record<string, number>;
    sort?: Sort[];
    filter?: Filter[];
    selection?: Map<number, Row>;
    ready?: boolean;
    flags?: Map<number, string[]>;
    onopen?: (row: Row) => void;
    cell?: Snippet<[Row, Col]>;
  } = $props();

  const CHECK = 28;
  const HEAD = 56;
  const OVERSCAN = 8;

  let viewport = $state<HTMLDivElement>();
  let scrollTop = $state(0);
  let height = $state(0);
  let active = $state(-1);
  let anchor = -1;

  const w = (c: Col) => widths[c.name] ?? defaultWidth(c);
  const total = $derived(columns.reduce((s, c) => s + w(c), CHECK));
  const bodyH = $derived(Math.max(0, height - HEAD - ROW - 2));
  const first = $derived(source.indexAt(scrollTop));
  const last = $derived(source.indexAt(scrollTop + bodyH));
  const visible = $derived.by(() => {
    const out: number[] = [];
    for (let i = Math.max(0, first - OVERSCAN); i <= Math.min(source.length - 1, last + OVERSCAN); i++) out.push(i);
    return out;
  });

  $effect(() => {
    void source;
    untrack(() => {
      active = -1;
      anchor = -1;
      if (viewport) viewport.scrollTop = 0;
    });
  });
  $effect(() => source.ensure(first - 40, last + 80));
  $effect(() => {
    ready = source.ready && (source.length === 0 || !!source.entry(first));
  });

  const aggKey = (c: Col) => {
    const a = source.q.agg.find((x) => x.col === c.name);
    return a && (a.fn === "count" ? "count" : `${a.fn}_${a.col}`);
  };
  // Leading columns without an aggregate are merged into one label cell on group and total rows.
  const lead = $derived.by(() => {
    const i = columns.findIndex((c) => aggKey(c));
    return i < 0 ? columns.length : i;
  });
  const leadWidth = $derived(columns.slice(0, lead).reduce((s, c) => s + w(c), CHECK));

  const idOf = (r: Row) => r.id as number | undefined;
  const allSelected = $derived(selection.size > 0 && selection.size >= (source.totals?.count as number));

  function rowsBetween(a: number, b: number) {
    const out = new Map<number, Row>();
    for (let i = Math.min(a, b); i <= Math.max(a, b); i++) {
      const e = source.entry(i);
      if (e?.kind === "row" && idOf(e.row) != null) out.set(idOf(e.row)!, e.row);
    }
    return out;
  }

  function select(i: number, mode: "only" | "toggle" | "range") {
    const e = source.entry(i);
    active = i;
    if (mode === "range" && anchor >= 0) {
      selection = rowsBetween(anchor, i);
      return;
    }
    anchor = i;
    if (e?.kind !== "row" || idOf(e.row) == null) return;
    const id = idOf(e.row)!;
    if (mode === "toggle") {
      const next = new Map(selection);
      if (!next.delete(id)) next.set(id, e.row);
      selection = next;
    } else selection = new Map([[id, e.row]]);
  }

  async function selectAll() {
    if (allSelected) selection = new Map();
    else selection = new Map((await source.all()).filter((r) => idOf(r) != null).map((r) => [idOf(r)!, r]));
  }

  function reveal(i: number) {
    if (!viewport) return;
    const y = source.top(i);
    if (y < scrollTop) viewport.scrollTop = y;
    else if (y + ROW > scrollTop + bodyH) viewport.scrollTop = y + ROW - bodyH;
  }

  function move(i: number, e: KeyboardEvent) {
    i = Math.max(0, Math.min(source.length - 1, i));
    if (e.ctrlKey || e.metaKey) active = i;
    else select(i, e.shiftKey ? "range" : "only");
    reveal(i);
  }

  function keydown(e: KeyboardEvent) {
    const target = e.target as HTMLElement;
    if (target.closest(".grid-head") || target.closest("input, textarea, [contenteditable]")) return;
    const page = Math.max(1, Math.floor(bodyH / ROW) - 1);
    const cur = source.entry(active);
    const keys: Record<string, () => void> = {
      ArrowDown: () => move(active + 1, e),
      ArrowUp: () => move(active - 1, e),
      PageDown: () => move(active + page, e),
      PageUp: () => move(active - page, e),
      Home: () => move(0, e),
      End: () => move(source.length - 1, e),
      " ": () => select(active, "toggle"),
      Enter: () => (cur?.kind === "group" ? source.toggle(cur.key) : cur && onopen?.(cur.row)),
      ArrowRight: () => cur?.kind === "group" && !cur.open && source.toggle(cur.key),
      ArrowLeft: () => cur?.kind === "group" && cur.open && source.toggle(cur.key),
      a: () => (e.ctrlKey || e.metaKey) && selectAll(),
    };
    const fn = keys[e.key];
    if (!fn) return;
    e.preventDefault();
    fn();
  }

  function clickRow(i: number, e: MouseEvent) {
    select(i, e.shiftKey ? "range" : e.ctrlKey || e.metaKey ? "toggle" : "only");
  }

  function sortBy(c: Col, e: MouseEvent) {
    const cur = sort.find((s) => s.col === c.name);
    const next = !cur ? { col: c.name, dir: "asc" as const } : cur.dir === "asc" ? { col: c.name, dir: "desc" as const } : null;
    const rest = e.shiftKey ? sort.filter((s) => s.col !== c.name) : [];
    sort = next ? [...rest, next] : rest;
  }

  function setFilter(c: Col, f: ColFilter | null) {
    filter = [...filter.filter((x) => !("col" in x) || x.col !== c.name), ...(f ? [f] : [])];
  }

  function titleIfTruncated(e: MouseEvent) {
    const c = (e.target as HTMLElement).closest<HTMLElement>(".cell");
    if (c) c.title = c.scrollWidth > c.clientWidth ? c.innerText : "";
  }

  function resize(c: Col, e: PointerEvent) {
    const grip = e.currentTarget as HTMLElement;
    const x0 = e.clientX;
    const w0 = w(c);
    grip.setPointerCapture(e.pointerId);
    grip.onpointermove = (m) => (widths = { ...widths, [c.name]: Math.max(40, w0 + m.clientX - x0) });
    grip.onpointerup = () => (grip.onpointermove = null);
    e.stopPropagation();
  }
</script>

{#snippet value(c: Col, v: unknown)}
  {#if c.type === "bool"}
    {#if v}<Check />{/if}
  {:else if c.ref === "file"}
    {#if v != null}<Attach />{/if}
  {:else}
    {display(c, v)}
  {/if}
{/snippet}

{#snippet aggCells(r: Row)}
  {#each columns.slice(lead) as c (c.name)}
    {@const k = aggKey(c)}
    {@const v = k ? r[k] : undefined}
    <div class="cell num" class:neg={typeof v === "number" && v < 0} style:width="{w(c)}px">
      {#if k}{display({ ...c, ref: undefined, enum: undefined }, v)}{/if}
    </div>
  {/each}
{/snippet}

<div
  class="grid"
  role="grid"
  tabindex="0"
  aria-rowcount={source.length}
  aria-multiselectable="true"
  onkeydown={keydown}
>
  <div
    class="viewport"
    bind:this={viewport}
    bind:clientHeight={height}
    onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
  >
    <div class="grid-head" style:width="{total}px">
      <div class="hrow" role="row">
        <div class="cell check" role="columnheader">
          <button class="checkbox" tabindex="-1" aria-label={t("ui.select_all")} data-state={allSelected ? "checked" : selection.size ? "indeterminate" : "unchecked"} onclick={selectAll}>
            {#if allSelected}<Check />{:else if selection.size}<Dash />{/if}
          </button>
        </div>
        {#each columns as c (c.name)}
          {@const s = sort.findIndex((x) => x.col === c.name)}
          <div class="cell hcell" role="columnheader" aria-sort={s < 0 ? "none" : sort[s].dir === "asc" ? "ascending" : "descending"} style:width="{w(c)}px">
            <button class="hbtn" class:num={isNumeric(c)} title={c.description ?? label(c)} onclick={(e) => sortBy(c, e)}>
              <span>{label(c)}</span>
              {#if s >= 0}
                {#if sort[s].dir === "asc"}<Up />{:else}<Down />{/if}
                {#if sort.length > 1}<sup>{s + 1}</sup>{/if}
              {/if}
            </button>
            <div class="grip" role="separator" aria-orientation="vertical" onpointerdown={(e) => resize(c, e)}></div>
          </div>
        {/each}
      </div>
      <div class="frow" role="row">
        <div class="cell check"></div>
        {#each columns as c (c.name)}
          <div class="cell fcell" style:width="{w(c)}px">
            <GridFilter col={c} filter={filter.find((f): f is ColFilter => "col" in f && f.col === c.name)} onchange={(f) => setFilter(c, f)} />
          </div>
        {/each}
      </div>
    </div>

    <!-- svelte-ignore a11y_mouse_events_have_key_events (a native title only, for truncated cells) -->
    <div class="body" role="rowgroup" style:height="{source.height}px" style:width="{total}px" onmouseover={titleIfTruncated}>
      {#each visible as i (i)}
        {@const e = source.entry(i)}
        {#if e?.kind === "group"}
          <!-- svelte-ignore a11y_click_events_have_key_events (keys are handled by the grid) -->
          <div
            class="row group"
            class:active={i === active}
            role="row"
            tabindex="-1"
            aria-expanded={e.open}
            style:height="{GROUP}px"
            style:transform="translateY({source.top(i)}px)"
            onclick={() => {
              active = i;
              source.toggle(e.key);
            }}
          >
            <div class="cell lead" style:width="{leadWidth}px">
              {#if e.open}<Open />{:else}<Closed />{/if}
              <span>{source.q.group.map((g) => display(columns.find((c) => c.name === g) ?? { name: g, type: "text", format: "", nullable: true }, e.row[g]) || "—").join(" · ")}</span>
              <Badge>{fmtNumber(e.count)}</Badge>
            </div>
            {@render aggCells(e.row)}
          </div>
        {:else if e}
          {@const id = idOf(e.row)}
          {@const sel = id != null && selection.has(id)}
          {@const flag = id != null ? flags?.get(id) : undefined}
          <!-- svelte-ignore a11y_click_events_have_key_events (keys are handled by the grid) -->
          <div
            class="row"
            class:selected={sel}
            class:active={i === active}
            class:flagged={flag}
            role="row"
            tabindex="-1"
            aria-selected={sel}
            data-id={id}
            style:transform="translateY({source.top(i)}px)"
            onclick={(ev) => clickRow(i, ev)}
            ondblclick={() => onopen?.(e.row)}
          >
            <div class="cell check" style:padding-left="{e.depth * 12}px">
              <button class="checkbox" tabindex="-1" aria-label={t("ui.select")} data-state={sel ? "checked" : "unchecked"} onclick={(ev) => { ev.stopPropagation(); select(i, "toggle"); }}>
                {#if sel}<Check />{/if}
              </button>
            </div>
            {#each columns as c, ci (c.name)}
              {@const v = e.row[c.name]}
              <div class="cell" class:num={isNumeric(c)} class:neg={typeof v === "number" && v < 0 && isNumeric(c)} style:width="{w(c)}px">
                {#if cell}{@render cell(e.row, c)}{:else if ci === 0 && flag}
                  <Tooltip text={flag.join(", ")}><span class="flag">{@render value(c, v)}</span></Tooltip>
                {:else}{@render value(c, v)}{/if}
              </div>
            {/each}
          </div>
        {:else}
          <div class="row loading" style:transform="translateY({source.top(i)}px)"><div class="cell" style:width="{total}px"><span class="skeleton"></span></div></div>
        {/if}
      {/each}
      {#if source.ready && source.length === 0}<div class="empty-state">{t("ui.no_rows")}</div>{/if}
    </div>

    <div class="total" role="row" style:width="{total}px">
      <div class="cell lead" style:width="{leadWidth}px">{t("ui.total")}<Badge>{fmtNumber((source.totals?.count as number) ?? 0)}</Badge></div>
      {#if source.totals}{@render aggCells(source.totals)}{/if}
    </div>
  </div>
</div>

<style>
  .grid {
    display: flex;
    flex: 1;
    min-width: 0;
    min-height: 0;
    font-size: var(--fs-grid);
    background: var(--surface);
    outline: none;
    user-select: none;
  }
  .grid:focus-visible .row.active {
    outline: 1px solid var(--accent);
    outline-offset: -1px;
  }
  .viewport {
    position: relative;
    flex: 1;
    overflow: auto;
    font-variant-numeric: tabular-nums;
  }
  .grid-head {
    position: sticky;
    top: 0;
    z-index: 2;
    min-width: 100%;
    background: var(--header);
  }
  .hrow,
  .frow,
  .row {
    display: flex;
    height: var(--h-row);
  }
  .hrow {
    height: var(--h-grid-header);
    border-bottom: var(--bw) solid var(--border);
  }
  .frow {
    height: var(--h-grid-header);
    border-bottom: var(--bw) solid var(--border-strong);
    background: var(--surface-subtle);
  }
  .cell {
    display: flex;
    align-items: center;
    flex: none;
    gap: var(--s-2);
    padding: var(--cell-pad);
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .cell.num {
    justify-content: flex-end;
  }
  .cell.check {
    width: 28px;
    padding: 0 0 0 7px;
  }
  .checkbox {
    width: 14px;
    height: 14px;
  }
  /* Row checkboxes show on hover, on selected rows and on the keyboard row (Space selects). */
  .row .checkbox {
    opacity: 0;
  }
  .row:hover .checkbox,
  .row.selected .checkbox,
  .grid:focus-visible .row.active .checkbox {
    opacity: 1;
  }
  .hcell {
    position: relative;
    padding: 0;
  }
  .hbtn {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    width: 100%;
    height: 100%;
    padding: var(--cell-pad);
    border: 0;
    background: transparent;
    font-weight: var(--fw-semibold);
    text-align: left;
    cursor: pointer;
  }
  .hbtn span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .hbtn.num {
    justify-content: flex-end;
  }
  .hbtn:hover {
    background: var(--hover);
  }
  .hbtn :global(svg) {
    width: 12px;
    height: 12px;
    color: var(--accent);
  }
  .hbtn sup {
    font-size: 9px;
    color: var(--accent);
  }
  .grip {
    position: absolute;
    right: 0;
    top: 4px;
    bottom: 4px;
    width: 5px;
    border-right: var(--bw) solid var(--border);
    cursor: col-resize;
  }
  .grip:hover {
    border-right: 2px solid var(--accent);
  }
  .fcell {
    padding: 0 var(--s-1);
  }
  .fcell :global(.input) {
    height: 22px;
    padding: 0 var(--s-3);
    border-color: transparent;
    background: transparent;
    font-size: var(--fs-grid);
  }
  .fcell :global(.input:hover),
  .fcell :global(.input:focus),
  .fcell :global(.input:focus-within) {
    border-color: var(--border);
    background: var(--surface);
  }
  .fcell :global(.combo) {
    padding-left: var(--s-3);
  }
  .fcell :global(.field-btn) {
    width: 18px;
    visibility: hidden;
  }
  .fcell:hover :global(.field-btn),
  .fcell:focus-within :global(.field-btn) {
    visibility: visible;
  }
  .fcell :global(.select .muted) {
    color: var(--text-disabled);
  }
  .body {
    position: relative;
    min-width: 100%;
  }
  .row {
    position: absolute;
    left: 0;
    right: 0;
    top: 0;
    border-bottom: var(--bw) solid var(--border);
    cursor: default;
  }
  .row:hover {
    background: var(--hover);
  }
  .row.selected {
    background: var(--selection);
  }
  .row.flagged {
    box-shadow: inset 3px 0 0 var(--negative);
    background: var(--negative-subtle);
  }
  .row.group {
    height: var(--h-group);
    background: var(--bg-app);
    font-weight: var(--fw-semibold);
    cursor: pointer;
  }
  .row.group :global(svg) {
    width: 12px;
    height: 12px;
    color: var(--text-muted);
  }
  .lead {
    position: sticky;
    left: 0;
    z-index: 1;
    gap: var(--s-3);
    background: inherit;
  }
  .lead span {
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .cell :global(svg) {
    width: 14px;
    height: 14px;
  }
  .flag {
    color: var(--negative);
  }
  .skeleton {
    display: block;
    width: 40%;
    height: 8px;
    border-radius: 4px;
    background: var(--header);
  }
  .total {
    position: sticky;
    bottom: 0;
    z-index: 1;
    display: flex;
    height: var(--h-group);
    min-width: 100%;
    border-top: var(--bw) solid var(--border-strong);
    background: var(--surface);
    font-weight: var(--fw-semibold);
  }
  .empty-state {
    padding: var(--s-8);
    color: var(--text-muted);
    text-align: center;
  }
  .neg {
    color: var(--negative);
  }
</style>

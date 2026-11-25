<script lang="ts">
  import type { Agg, Col, Filter, Row, Tile } from "../api/contract";
  import NumberChart from "../chart/Number.svelte";
  import Bar from "../chart/Bar.svelte";
  import Line from "../chart/Line.svelte";
  import Donut from "../chart/Donut.svelte";
  import Progress from "../chart/Progress.svelte";
  import Heatmap from "../chart/Heatmap.svelte";
  import Table from "../chart/Table.svelte";
  import { num, type Axis, type Series } from "../chart/chart";
  import { follow } from "../app/cache.svelte";
  import { display } from "../app/display";
  import { resolve } from "../app/params";
  import { go } from "../app/router.svelte";
  import { colLabel, session, view } from "../app/session.svelte";
  import { fmtDate, fmtNumber } from "../ds/format";
  import { has, i18n, t } from "../labels/i18n.svelte";

  // A tile (SPEC §20 Dashboard): its view read through the cache with its wrap, drawn. The first group is the axis,
  // the second the series; without a second group, each aggregate is a series.
  let { tile, params, onshown }: { tile: Tile; params: Record<string, unknown>; onshown: () => void } = $props();

  type Group = NonNullable<NonNullable<Tile["wrap"]>["group"]>[number];
  const v = $derived(view(tile.view)!);
  const wrap = $derived(tile.wrap ?? {});
  const [g0, g1] = $derived(wrap.group ?? []);
  const aggs = $derived(wrap.agg ?? []);
  // The screen's params the view has, then the tile's own.
  const own = $derived({
    ...Object.fromEntries(v.params.filter((p) => p.name in params).map((p) => [p.name, params[p.name]])),
    ...Object.fromEntries(Object.entries(tile.params ?? {}).map(([k, e]) => [k, resolve(e, session.me)])),
  });

  let rows = $state.raw<Row[] | null>(null);
  let failed = $state(false);
  $effect(() =>
    follow(
      [[tile.view, { ...wrap, params: own }]],
      ([r]) => {
        rows = r;
        failed = false;
        onshown();
      },
      () => {
        failed = true;
        onshown();
      },
    ),
  );

  const colOf = (name: string) => v.columns.find((c) => c.name === name)!;
  const keyOf = (g: Group) => (typeof g === "string" ? g : `${g.col}_${g.by}`);
  const aggKey = (a: Agg) => (a.fn === "count" ? "count" : `${a.fn}_${a.col}`);
  // An aggregate has the format of its column; a count is a plain number.
  const aggCol = (a: Agg): Col => (a.fn === "count" ? { name: "count", type: "int8", format: "", nullable: false } : { ...colOf(a.col), name: aggKey(a) });
  const aggLabel = (a: Agg) => (a.fn === "count" ? t("ui.count") : colLabel(a.col));
  const month = $derived(new Intl.DateTimeFormat(`${i18n.lang}-CH`, { month: "short", year: "2-digit" }));
  const period: Record<string, (iso: string) => string> = {
    day: fmtDate,
    week: (iso) => fmtDate(iso).slice(0, 5),
    month: (iso) => month.format(new Date(`${iso}T00:00`)),
    year: (iso) => iso.slice(0, 4),
  };
  const fmtOf = (g: Group) => (x: unknown) =>
    x == null ? "—" : typeof g === "string" ? display(colOf(g), x) || String(x) : period[g.by](String(x));
  const series = (c: Col, key: string, label: string): Series => ({
    key, label, fmt: (n) => display(c, n), tick: (n) => (c.format === "money" ? fmtNumber(n) : display(c, n)), unit: c.format === "min" ? 60 : 1,
  });

  const axis = $derived<Axis | undefined>(g0 ? { key: keyOf(g0), fmt: fmtOf(g0) } : undefined);
  // Rows by axis value, one key per series: the second group's values pivoted, or the aggregates.
  const data = $derived.by(() => {
    if (!rows) return null;
    if (!g1 || !g0) return { rows, series: aggs.map((a) => series(aggCol(a), aggKey(a), aggLabel(a))) };
    const [k0, k1, a] = [keyOf(g0), keyOf(g1), aggs[0]];
    const values = [...new Set(rows.map((r) => r[k1]))];
    const wide = new Map<unknown, Row>();
    for (const r of rows) wide.set(r[k0], { ...(wide.get(r[k0]) ?? { [k0]: r[k0] }), [`s${values.indexOf(r[k1])}`]: r[aggKey(a)] });
    return { rows: [...wide.values()], series: values.map((x, i) => series(aggCol(a), `s${i}`, fmtOf(g1)(x))) };
  });
  // A table shows the groups and the aggregates, or the view's columns.
  const table = $derived<[Col, string][]>(
    g0 || aggs.length
      ? [
          ...(wrap.group ?? []).map((g): [Col, string] => {
            const c = colOf(typeof g === "string" ? g : g.col);
            return [{ ...c, name: keyOf(g) }, colLabel(c.name)];
          }),
          ...aggs.map((a): [Col, string] => [aggCol(a), aggLabel(a)]),
        ]
      : v.columns.filter((c) => c.name !== "id").map((c): [Col, string] => [c, colLabel(c.name)]),
  );

  // The linked screen, with the tile's params and filter that its view has; a mark adds its axis value (a plain column).
  const target = $derived(session.meta.screens.find((s) => s.id === tile.link));
  function open(extra: Filter[] = []) {
    const tv = target && view(target.views[0]);
    if (!target || !tv) return;
    const hasCol = (c: string) => tv.columns.some((x) => x.name === c);
    go(target.id, {
      params: Object.fromEntries(Object.entries(own).filter(([k]) => tv.params.some((p) => p.name === k))),
      wrap: { filter: [...(wrap.filter ?? []), ...extra].filter((f) => !("col" in f) || hasCol(f.col)) },
    });
  }
  const pick = $derived(
    target && typeof g0 === "string" ? (r: Row) => open([r[g0] == null ? { col: g0, op: "null" } : { col: g0, op: "eq", value: r[g0] }]) : undefined,
  );
  const title = $derived(has(tile.title) ? t(tile.title) : tile.title);
</script>

<article class="tile" style:--span={tile.span ?? 1} data-tile={tile.title} data-state={failed ? "error" : !data ? "loading" : data.rows.length ? "shown" : "empty"}>
  <header>
    {#if target}<button class="title link" onclick={() => open()}>{title}</button>{:else}<h3 class="title">{title}</h3>{/if}
  </header>
  {#if failed}
    <p class="note">{t("ui.tile_error")}</p>
  {:else if !data}
    <div class="skeleton"></div>
  {:else if !data.rows.length}
    <p class="note">{t("ui.no_data")}</p>
  {:else if tile.form === "number"}
    {@const [a, b] = data.series}
    <NumberChart value={a.fmt(num(data.rows[0], a.key))} secondary={b && `${b.label} ${b.fmt(num(data.rows[0], b.key))}`} />
  {:else if tile.form === "table" || !axis}
    <Table rows={data.rows} columns={table.map(([c]) => c)} label={(c) => table.find(([x]) => x === c)![1]} {title} onpick={pick} />
  {:else if tile.form === "bar"}
    <Bar rows={data.rows} x={axis} series={data.series} {title} horizontal={tile.horizontal} stacked={tile.stacked} onpick={pick} />
  {:else if tile.form === "line"}
    <Line rows={data.rows} x={axis} series={data.series} {title} onpick={pick} />
  {:else if tile.form === "donut"}
    <Donut rows={data.rows} x={axis} value={data.series[0]} {title} onpick={pick} />
  {:else if tile.form === "progress"}
    <Progress rows={data.rows} x={axis} actual={data.series[0]} target={data.series[1]} {title} onpick={pick} />
  {:else if tile.form === "heatmap"}
    <Heatmap rows={data.rows} x={axis} value={data.series[0]} {title} onpick={pick} />
  {/if}
</article>

<style>
  .tile {
    grid-column: span var(--span);
    display: flex;
    flex-direction: column;
    gap: var(--s-4);
    min-width: 0;
    padding: var(--panel-pad);
    border: var(--bw) solid var(--border);
    border-radius: var(--r-button);
    background: var(--surface);
  }
  .title {
    margin: 0;
    padding: 0;
    border: 0;
    background: none;
    color: var(--text);
    font: inherit;
    font-size: var(--fs-section);
    font-weight: var(--fw-semibold);
    text-align: left;
  }
  .link {
    cursor: pointer;
  }
  .link:hover {
    color: var(--accent);
    text-decoration: underline;
  }
  .note {
    margin: 0;
    color: var(--text-muted);
  }
  .skeleton {
    height: 48px;
    border-radius: var(--r-input);
    background: var(--surface-subtle);
  }
  @media (max-width: 1100px) {
    .tile {
      grid-column: span min(var(--span), 2);
    }
  }
</style>

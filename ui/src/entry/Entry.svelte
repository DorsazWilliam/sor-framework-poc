<script lang="ts">
  import { tick, untrack } from "svelte";
  import type { Screen } from "../api/contract";
  import CommandBar from "../ds/CommandBar.svelte";
  import Button from "../ds/Button.svelte";
  import IconButton from "../ds/IconButton.svelte";
  import Select from "../ds/Select.svelte";
  import Drawer from "../ds/Drawer.svelte";
  import Tabs from "../ds/Tabs.svelte";
  import UserMenu from "../shell/UserMenu.svelte";
  import ViewScreen from "../screen/ViewScreen.svelte";
  import History from "../screen/History.svelte";
  import EntryLine from "./EntryLine.svelte";
  import { Entry, Line, addDays, forgetAllowed } from "./store.svelte";
  import Prev from "~icons/fluent/chevron-left-16-regular";
  import Next from "~icons/fluent/chevron-right-16-regular";
  import Copy from "~icons/fluent/copy-16-regular";
  import Delete from "~icons/fluent/delete-16-regular";
  import Panel from "~icons/fluent/panel-right-16-regular";
  import Locked from "~icons/fluent/lock-closed-12-regular";
  import Holiday from "~icons/fluent/star-12-regular";
  import { watch } from "../app/live.svelte";
  import { lookup } from "../app/lookups.svelte";
  import { colLabel, command, screenLabel, session, table, view } from "../app/session.svelte";
  import { evaluate } from "../screen/commands";
  import { fmtDate, fmtDuration, isoDate } from "../ds/format";
  import { i18n, t } from "../labels/i18n.svelte";

  // Hand-made keyboard entry screen (SPEC §20): week strip, editing grid grouped by day, totals, drawer.
  let { screen }: { screen: Screen } = $props();

  const store = new Entry();
  const today = isoDate(new Date());
  const root = $derived(table(screen.table!)!.root);
  let grid = $state<HTMLDivElement>();
  let focus: { uid: number; id: number | null; col: string; date: string } | null = null;
  let pending: string | null = store.day; // date whose fresh line takes focus once displayed
  let current = $state<Line | null>(null);
  let drawer = $state(false);
  let drawerWidth = $state(420);
  let tab = $state(untrack(() => screen.children[0]?.id) ?? "history");

  $effect(() => {
    void store.user;
    void store.monday;
    untrack(() => store.refresh());
  });
  $effect(() => watch([...new Set(screen.views.flatMap((v) => view(v)?.roots ?? []))], () => store.refresh()));
  $effect(() => watch(view("folder_task_allowed")?.roots ?? [], forgetAllowed));
  $effect(() => {
    session.status = { rows: store.lines.filter((l) => l.id != null && store.dates.includes(l.date)).length, selected: 0 };
  });

  // Effective start of each line: its own, else the end of the line above, else 08:00.
  function starts(lines: Line[]) {
    let prev = 480;
    return lines.map((l) => {
      const s = l.draft.start_tod ?? prev;
      prev = s + (l.draft.duration_min ?? 0);
      return s;
    });
  }
  const linesOf = (date: string) => store.lines.filter((l) => l.date === date);
  const startOf = (l: Line) => {
    const ls = linesOf(l.date);
    return starts(ls)[ls.indexOf(l)];
  };
  const groups = $derived(
    store.dates.map((date) => {
      const lines = linesOf(date);
      return { date, lines, starts: starts(lines), day: store.dayRow(date) };
    }),
  );
  const sum = (k: string) => store.days.reduce((s, d) => s + ((d[k] as number) ?? 0), 0);
  const day = $derived(store.dayRow(store.day));
  const balance = $derived(sum("done_min") - sum("due_min"));
  const users = $derived((session.me.reach.user ?? []).map((id) => ({ value: String(id), label: lookup("user", id) ?? "" })));
  const tabs = $derived([...screen.children.map((c) => ({ value: c.id, label: screenLabel(c) })), { value: "history", label: t("ui.history") }]);
  const canDelete = $derived(!!current?.row && evaluate(command("activity.delete")!, [current.row]).enabled);

  const lineOf = (uid: number) => store.lines.find((l) => l.uid === uid);
  const complete = (l: Line) => l.draft.duration_min != null && l.draft.folder_id != null && l.draft.task_id != null;
  const inGrid = () => !!grid && grid.contains(document.activeElement);

  async function focusCell(l: Line | undefined, col: string) {
    await tick();
    const el = l && grid?.querySelector<HTMLElement>(`[data-uid="${l.uid}"]`);
    (el?.querySelector<HTMLElement>(`[data-col="${col}"] input`) ?? el?.querySelector<HTMLElement>("input") ?? el)?.focus();
  }

  // Focus is restored after a refresh or a day change: by line, then by id, then the day's fresh line.
  $effect(() => {
    void store.lines;
    void store.day;
    void store.dates;
    tick().then(() => {
      if (!store.ready) return;
      if (pending) {
        const l = store.freshLine(pending);
        if (l && store.dates.includes(pending)) {
          focusCell(l, "start_tod");
          pending = null;
        }
      } else if (focus && !inGrid() && (!document.activeElement || document.activeElement === document.body)) {
        const f = focus;
        const l = lineOf(f.uid) ?? store.lines.find((x) => x.id != null && x.id === f.id) ?? store.freshLine(f.date);
        if (l && store.dates.includes(l.date)) focusCell(l, f.col);
      }
    });
  });

  function onfocusin(e: FocusEvent) {
    const target = e.target as HTMLElement;
    const el = target.closest<HTMLElement>("[data-uid]");
    const l = el && lineOf(+el.dataset.uid!);
    if (!l) return;
    focus = { uid: l.uid, id: l.id, col: target.closest<HTMLElement>("[data-col]")?.dataset.col ?? "start_tod", date: l.date };
    current = l;
  }

  // Leaving a line saves it; a complete fresh line becomes a line and a new fresh line opens.
  function leave(l: Line) {
    if (!store.lines.includes(l)) return;
    if (l.fresh && complete(l)) store.promote(l);
    store.save(l, startOf(l));
  }

  function enter(l: Line, col: string) {
    const ls = linesOf(l.date);
    if (l.fresh) {
      const missing = (["duration_min", "folder_id", "task_id"] as const).find((f) => l.draft[f] == null);
      if (missing) return focusCell(l, missing);
      leave(l);
      return focusCell(store.freshLine(l.date), "start_tod");
    }
    leave(l);
    const next = ls[ls.indexOf(l) + 1];
    focusCell(next, next?.fresh ? "start_tod" : col);
  }

  function duplicate(l: Line) {
    (document.activeElement as HTMLElement | null)?.blur();
    focusCell(store.duplicate(l), "start_tod");
  }

  async function remove(l: Line, col: string) {
    const ls = linesOf(l.date);
    const next = ls[ls.indexOf(l) + 1] ?? ls[ls.indexOf(l) - 1];
    await store.remove(l);
    focusCell(store.lines.includes(l) ? l : next, col);
  }

  function gridKeys(e: KeyboardEvent) {
    const target = e.target as HTMLElement;
    const el = target.closest<HTMLElement>("[data-uid]");
    const l = el && lineOf(+el.dataset.uid!);
    if (!l || e.defaultPrevented || target.getAttribute("aria-expanded") === "true") return;
    const col = target.closest<HTMLElement>("[data-col]")?.dataset.col ?? "start_tod";
    const plain = !e.ctrlKey && !e.altKey && !e.shiftKey && !e.metaKey;
    if (e.key === "Enter" && plain) enter(l, col);
    else if (e.key === "Escape" && plain) {
      l.revert();
      focusCell(l, col);
    } else if (e.ctrlKey && e.key.toLowerCase() === "d") duplicate(l);
    else if (e.ctrlKey && e.key === "Delete") remove(l, col);
    else return;
    e.preventDefault();
  }

  // A day or week move first blurs the focused input: it commits what is typed and saves its line.
  function go(date: string) {
    const follow = inGrid() || pending != null;
    if (inGrid()) (document.activeElement as HTMLElement).blur();
    if (follow) pending = date;
    store.day = date;
  }

  const steps: Record<string, number> = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };
  function windowKeys(e: KeyboardEvent) {
    if (e.altKey && !e.ctrlKey && e.key in steps) go(addDays(store.day, steps[e.key]));
    else if (e.key === "F4" && !e.altKey) drawer = !drawer;
    else return;
    e.preventDefault();
  }

  const dayName = (d: string, weekday: "short" | "long") =>
    new Intl.DateTimeFormat(`${i18n.lang}-CH`, { weekday }).format(new Date(`${d}T12:00`));
  const dm = (d: string) => fmtDate(d).slice(0, 6);
  const hm = (r: { [k: string]: unknown } | undefined, k: string) => fmtDuration((r?.[k] as number) ?? 0);
  function isoWeek(iso: string) {
    const d = new Date(`${iso}T12:00`);
    d.setDate(d.getDate() + 3 - ((d.getDay() + 6) % 7));
    return 1 + Math.floor((+d - +new Date(d.getFullYear(), 0, 1)) / 864e5 / 7);
  }
  const linked = (c: Screen) => Object.fromEntries(Object.entries(c.link ?? {}).map(([p, col]) => [p, current!.row![col]]));
</script>

<svelte:window onkeydown={windowKeys} />

<section class="entry" data-screen={screen.id} data-ready={store.ready || undefined}>
  <CommandBar title={screenLabel(screen)}>
    <Button variant="subtle" icon={Copy} title="{t('ui.entry.duplicate')} (Ctrl+D)" disabled={!current || !evaluate(command("activity.create")!, []).enabled} onclick={() => current && duplicate(current)}>
      {t("ui.entry.duplicate")}
    </Button>
    <Button variant="subtle" icon={Delete} title="{t('ui.delete')} (Ctrl+Del)" disabled={!canDelete} onclick={() => current && remove(current, "start_tod")}>
      {t("ui.delete")}
    </Button>
    <Button variant="subtle" icon={Panel} title="{t('ui.entry.details')} (F4)" aria-pressed={drawer} onclick={() => (drawer = !drawer)}>
      {t("ui.entry.details")}
    </Button>
    {#snippet end()}<UserMenu />{/snippet}
  </CommandBar>

  <div class="bar">
    <IconButton icon={Prev} label={t("ui.entry.prev_week")} onclick={() => go(addDays(store.day, -7))} />
    <IconButton icon={Next} label={t("ui.entry.next_week")} onclick={() => go(addDays(store.day, 7))} />
    <Button onclick={() => go(today)}>{t("ui.entry.today")}</Button>
    <span class="week">{t("ui.entry.week_n", { n: isoWeek(store.monday) })}<span class="muted">&ensp;{dm(store.week[0])} – {fmtDate(store.week[6])}</span></span>
    {#if users.length > 1}
      <label class="user">
        <span class="muted">{colLabel("user_id", root)}</span>
        <Select id="entry-user" items={users} bind:value={() => String(store.user), (v) => (store.user = +v)} />
      </label>
    {/if}
    <div class="spacer"></div>
    <div class="seg" role="group">
      {#each ["day", "week"] as const as m (m)}
        <button type="button" aria-pressed={store.mode === m} onclick={() => (store.mode = m)}>{t(`ui.entry.${m}`)}</button>
      {/each}
    </div>
  </div>

  <div class="strip">
    {#each store.week as d (d)}
      {@const r = store.dayRow(d)}
      {@const due = (r?.due_min as number) ?? 0}
      {@const done = (r?.done_min as number) ?? 0}
      <button
        type="button"
        class="day"
        class:off={!due}
        aria-pressed={d === store.day}
        aria-current={d === today ? "date" : undefined}
        data-date={d}
        onclick={() => go(d)}
      >
        <span class="dname">
          <span>{dayName(d, "short")} {dm(d)}</span>
          {#if r?.holiday}<span class="mark" title={String(r.holiday)}><Holiday /></span>{/if}
          {#if r?.locked}<span class="mark" title={t("ui.entry.locked")}><Locked /></span>{/if}
        </span>
        <span class="dsum"><b>{fmtDuration(done)}</b> / {fmtDuration(due)}</span>
        <span class="meter"><span class:full={due > 0 && done >= due} style:width="{due ? Math.min(100, (100 * done) / due) : 0}%"></span></span>
      </button>
    {/each}
  </div>

  <div class="content">
    <div class="main">
      <div class="lines" bind:this={grid} onfocusin={onfocusin} onkeydown={gridKeys} role="grid" tabindex="-1">
        <div class="head">
          <div></div>
          <div class="num">{colLabel("start_tod", root)}</div>
          <div class="num">{colLabel("duration_min", root)}</div>
          <div class="num">{t("ui.entry.end")}</div>
          <div>{colLabel("folder_id", root)}</div>
          <div>{colLabel("task_id", root)}</div>
          <div class="num">{colLabel("billed_min", root)}</div>
          <div>{colLabel("description", root)}</div>
          <div>{t("ui.entry.state")}</div>
          <div class="num">{colLabel("amount", root)}</div>
        </div>
        {#each groups as g (g.date)}
          {#if store.mode === "week"}
            <div class="dayhead" class:today={g.date === today} data-date={g.date}>
              <span class="dlabel">{dayName(g.date, "long")} {dm(g.date)}</span>
              {#if g.day?.holiday}<span class="muted">{g.day.holiday}</span>{/if}
              {#if g.day?.locked}<span class="muted lock"><Locked />{t("ui.entry.locked")}</span>{/if}
              <span class="dtotal"><b>{hm(g.day, "done_min")}</b> / {hm(g.day, "due_min")}</span>
            </div>
          {/if}
          {#each g.lines as l, i (l.uid)}
            <EntryLine line={l} start={g.starts[i]} active={current === l} onleave={leave} />
          {/each}
        {/each}
      </div>
      <footer class="totals">
        <span>{t("ui.entry.day")} <b>{hm(day, "done_min")}</b> / {hm(day, "due_min")}</span>
        <span>{t("ui.entry.week")} <b>{fmtDuration(sum("done_min"))}</b> / {fmtDuration(sum("due_min"))}</span>
        <span>{t("ui.entry.balance")} <b class:neg={balance < 0}>{balance > 0 ? "+" : ""}{fmtDuration(balance)}</b></span>
        <span class="keys">{t("ui.entry.keys")}</span>
      </footer>
    </div>

    {#if drawer}
      <Drawer
        title={current?.id != null ? `${dayName(current.date, "short")} ${dm(current.date)} · ${lookup("folder", current.draft.folder_id ?? 0) ?? ""}` : t("ui.entry.details")}
        bind:width={drawerWidth}
        onclose={() => (drawer = false)}
      >
        {#if current?.id != null && current.row}
          <Tabs bind:value={tab} items={tabs}>
            {#snippet panel(v)}
              {#if v === "history"}
                <History {root} id={current!.id!} />
              {:else}
                {@const child = screen.children.find((c) => c.id === v)!}
                {#key current!.id}<ViewScreen screen={child} link={linked(child)} parent={current!.row!} />{/key}
              {/if}
            {/snippet}
          </Tabs>
        {:else}
          <p class="hint muted">{t("ui.entry.no_line")}</p>
        {/if}
      </Drawer>
    {/if}
  </div>
</section>

<style>
  .entry {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    min-height: var(--h-params);
    padding: var(--s-1) var(--s-4) var(--s-1) var(--s-5);
    border-bottom: var(--bw) solid var(--border);
    background: var(--surface);
  }
  .week {
    margin: 0 var(--s-6) 0 var(--s-4);
    font-weight: var(--fw-semibold);
    white-space: nowrap;
  }
  .week .muted {
    font-weight: normal;
  }
  .user {
    display: flex;
    align-items: center;
    gap: var(--s-3);
    width: 260px;
    white-space: nowrap;
  }
  .spacer {
    flex: 1;
  }
  .seg {
    display: flex;
    padding: 2px;
    border: var(--bw) solid var(--border);
    border-radius: var(--r-button);
    background: var(--surface-subtle);
  }
  .seg button {
    height: 22px;
    padding: 0 var(--s-5);
    border: 0;
    border-radius: 3px;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
  }
  .seg button[aria-pressed="true"] {
    background: var(--surface);
    color: var(--text);
    font-weight: var(--fw-semibold);
    box-shadow: 0 0 0 1px var(--border);
  }
  .strip {
    display: grid;
    grid-template-columns: repeat(7, minmax(0, 1fr));
    border-bottom: var(--bw) solid var(--border);
    background: var(--surface);
  }
  .day {
    position: relative;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 1px;
    padding: var(--s-3) var(--s-5) var(--s-4);
    border: 0;
    border-right: var(--bw) solid var(--border);
    background: transparent;
    text-align: left;
    cursor: pointer;
    font-variant-numeric: tabular-nums;
  }
  .day:last-child {
    border-right: 0;
  }
  .day:hover {
    background: var(--hover);
  }
  .day[aria-pressed="true"] {
    background: var(--selection);
  }
  .day[aria-pressed="true"]::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    bottom: -1px;
    height: 2px;
    background: var(--accent);
  }
  .day.off {
    background: var(--surface-subtle);
  }
  .day.off[aria-pressed="true"] {
    background: var(--selection);
  }
  .dname {
    display: flex;
    align-items: center;
    gap: var(--s-2);
    color: var(--text-muted);
    font-size: var(--fs-caption);
    text-transform: capitalize;
  }
  .day[aria-current="date"] .dname {
    color: var(--accent);
    font-weight: var(--fw-semibold);
  }
  .mark {
    display: flex;
    color: var(--text-muted);
  }
  .mark :global(svg) {
    width: 12px;
    height: 12px;
  }
  .dsum {
    color: var(--text-muted);
    font-size: var(--fs-grid);
  }
  .dsum b {
    color: var(--text);
    font-size: var(--fs-body);
  }
  .meter {
    width: 100%;
    height: 3px;
    margin-top: var(--s-2);
    border-radius: 2px;
    background: var(--header);
    overflow: hidden;
  }
  .meter span {
    display: block;
    height: 100%;
    background: var(--accent);
  }
  .meter span.full {
    background: var(--positive);
  }
  .content {
    display: flex;
    flex: 1;
    min-height: 0;
  }
  .main {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
  }
  .lines {
    --cols: 22px 56px 56px 48px minmax(150px, 300px) minmax(110px, 180px) 56px minmax(110px, 1fr) 56px 84px;
    flex: 1;
    overflow: auto;
    background: var(--surface);
    font-size: var(--fs-grid);
    font-variant-numeric: tabular-nums;
  }
  .head {
    position: sticky;
    top: 0;
    z-index: 2;
    display: grid;
    grid-template-columns: var(--cols);
    height: var(--h-grid-header);
    border-bottom: var(--bw) solid var(--border-strong);
    background: var(--header);
    font-weight: var(--fw-semibold);
  }
  .head > div {
    display: flex;
    align-items: center;
    padding: var(--cell-pad);
    overflow: hidden;
    white-space: nowrap;
  }
  .head > .num {
    justify-content: flex-end;
  }
  .dayhead {
    display: flex;
    align-items: center;
    gap: var(--s-5);
    height: var(--h-group);
    padding: 0 var(--s-4) 0 calc(22px + var(--s-4));
    border-bottom: var(--bw) solid var(--border);
    background: var(--bg-app);
  }
  .dlabel {
    font-weight: var(--fw-semibold);
    text-transform: capitalize;
  }
  .dayhead.today .dlabel {
    color: var(--accent);
  }
  .lock {
    display: flex;
    align-items: center;
    gap: var(--s-2);
  }
  .lock :global(svg) {
    width: 12px;
    height: 12px;
  }
  .dtotal {
    margin-left: auto;
    color: var(--text-muted);
  }
  .dtotal b {
    color: var(--text);
  }
  .totals {
    display: flex;
    align-items: center;
    gap: var(--s-7);
    height: var(--h-group);
    padding: 0 var(--s-5) 0 calc(22px + var(--s-4));
    border-top: var(--bw) solid var(--border-strong);
    background: var(--surface);
    font-variant-numeric: tabular-nums;
    color: var(--text-muted);
  }
  .totals b {
    color: var(--text);
    font-weight: var(--fw-semibold);
  }
  .totals b.neg {
    color: var(--negative);
  }
  .keys {
    margin-left: auto;
    font-size: var(--fs-caption);
  }
  .hint {
    padding: var(--panel-pad);
  }
</style>

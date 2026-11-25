// Browser-side performance harness (SPEC §23) against the running stack.
//   npm run perf [-- B1,B3,...]     SOR_URL (default http://localhost:8080), RUNS (default 20), OUT (JSON file)
// Each scenario runs RUNS + 1 times, the first run is discarded. Times are measured in the page, from the user
// action (click, key, commit) to the screen's marker (data-fresh: confirmed by the server; data-ready: displayed,
// from the client cache or the server; data-saved, a displayed row). Server-Timing of
// every /api/v and /api/c request involved gives the DB time, the round trips and the server time; a view request
// with aggregates and no group is named "<view> totals".
// Between measures, sessions park on the company screen: every measured user sees it and its data does not change.
// The dataset stays reusable: what a scenario creates is deleted, in a far-future week for activities.
import { chromium, type BrowserContext, type Page } from "playwright";
import { writeFileSync } from "node:fs";

const base = process.env.SOR_URL ?? "http://localhost:8080";
const RUNS = +(process.env.RUNS ?? 20);
const only = process.argv[2]?.split(",");
const out = process.env.OUT ?? `results-${new Date().toISOString().slice(0, 19).replace(/:/g, "")}.json`;

type Req = { name: string; db: number; rt: number; app: number };
type Sample = { ms: number; requests: Req[] };
type Result = { id: string; scenario: string; budget: number; user: string; samples: Sample[]; error?: string };

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const today = new Date();
const daysAgo = (n: number) => iso(new Date(today.getFullYear(), today.getMonth(), today.getDate() - n));
// The perf week: five years ahead, a Monday; activities created there are deleted at the end.
const perfMonday = (() => {
  const d = new Date(today.getFullYear() + 5, today.getMonth(), today.getDate());
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7));
})();
const perfDay = (n: number) => iso(new Date(perfMonday.getFullYear(), perfMonday.getMonth(), perfMonday.getDate() + n));

const browser = await chromium.launch();

// In the page: Server-Timing of the API requests started since t0, and a wait for a condition checked on every DOM change.
const helpers = () => {
  const w = window as unknown as Record<string, unknown>;
  const log: (Req & { t: number })[] = [];
  const fetch0 = window.fetch;
  window.fetch = async (input, init) => {
    const t = performance.now();
    const res = await fetch0(input, init);
    const path = new URL(input instanceof Request ? input.url : String(input), location.href).pathname;
    if (/^\/api\/[vc]\//.test(path)) {
      const st = Object.fromEntries((res.headers.get("server-timing") ?? "").split(",").map((m) => {
        const [n, ...kv] = m.trim().split(";");
        return [n, Object.fromEntries(kv.map((x) => x.split("=")))];
      }));
      const body = typeof init?.body === "string" ? JSON.parse(init.body) : {};
      const totals = path.startsWith("/api/v/") && body.agg?.length && !body.group?.length ? " totals" : "";
      log.push({ t, name: path.slice(5) + totals, db: +(st.db?.dur ?? 0), rt: parseInt((st.db?.desc ?? "0").replace('"', "")), app: +(st.app?.dur ?? 0) });
    }
    return res;
  };
  w.__requests = (t0: number) => log.filter((q) => q.t >= t0).map(({ t, ...q }) => q);
  // the first screen displayed after the page's navigation start (a reload)
  new MutationObserver((_, o) => {
    if (document.querySelector("[data-screen][data-ready]")) {
      w.__shown = performance.now();
      o.disconnect();
    }
  }).observe(document, { subtree: true, childList: true, attributes: true });
  w.__until = (cond: () => boolean) =>
    new Promise<number>((res, rej) => {
      if (cond()) return res(performance.now());
      setTimeout(() => rej(new Error("timeout (60 s)")), 60000);
      const o = new MutationObserver(() => {
        if (cond()) {
          o.disconnect();
          res(performance.now());
        }
      });
      o.observe(document, { subtree: true, childList: true, attributes: true, characterData: true });
    });
};

async function login(user: string) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  await ctx.addInitScript(helpers);
  const page = await ctx.newPage();
  await page.goto(`${base}/`);
  await page.fill('input[name="username"]', user);
  await page.click('input[type="submit"]');
  await page.waitForURL((u) => u.pathname === "/" && !u.port.startsWith("809"));
  await page.goto(`${base}/${hash("company")}`);
  await page.waitForSelector('[data-screen="company"][data-fresh]');
  return { ctx, page, user };
}
type Session = Awaited<ReturnType<typeof login>>;

async function post<T = Record<string, unknown>[]>(ctx: BrowserContext, path: string, body: unknown): Promise<T> {
  const r = await ctx.request.post(`${base}${path}`, { data: body });
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`);
  return r.json();
}
const view = (s: Session, v: string, body: object) => post(s.ctx, `/api/v/${v}`, body);
const call = (s: Session, c: string, body: object) => post<{ ids: number[] }>(s.ctx, `/api/c/${c}`, body);
const hash = (screen: string, state?: object) => `#/${screen}${state ? `?s=${encodeURIComponent(JSON.stringify(state))}` : ""}`;

// Runs `act` in the page (a link click, a button click...) and waits for the selector; returns the elapsed time.
async function measure(page: Page, act: string, selector: string, cond?: string): Promise<Sample> {
  return page.evaluate(
    async ([act, selector, cond]) => {
      const w = window as unknown as { __requests: (t0: number) => Req[]; __until: (c: () => boolean) => Promise<number>; __t0: number };
      const t0 = (w.__t0 = performance.now());
      new Function(act)();
      const t1 = await w.__until(() => {
        const el = document.querySelector(selector);
        return !!el && (!cond || new Function("el", cond)(el));
      });
      await new Promise((r) => setTimeout(r, 30)); // late requests of the same action
      return { ms: t1 - t0, requests: w.__requests(t0) };
    },
    [act, selector, cond ?? ""] as const,
  );
}
const link = (h: string) => `const a = document.createElement("a"); a.href = ${JSON.stringify(h)}; document.body.append(a); a.click(); a.remove();`;
const away = async (page: Page) => {
  await page.evaluate((h) => (location.hash = h), hash("company"));
  await page.waitForSelector('[data-screen="company"][data-fresh]');
};
// Clicks a link to a screen (from another screen) until the screen shows the server's data: a reload first makes
// every entry of the client cache stale, so the server path is measured.
const open = async (page: Page, screen: string, state?: object) => {
  await away(page);
  await page.reload();
  await page.waitForSelector('[data-screen="company"][data-fresh]');
  return measure(page, link(hash(screen, state)), `[data-screen="${screen}"][data-fresh]`);
};

const results: Result[] = [];
async function scenario(id: string, name: string, budget: number, s: Session, run: (i: number) => Promise<Sample>, setup?: () => Promise<void>, teardown?: () => Promise<void>) {
  if (only && !only.includes(id)) return;
  const r: Result = { id, scenario: name, budget, user: s.user, samples: [] };
  results.push(r);
  try {
    await setup?.();
    for (let i = 0; i <= RUNS; i++) {
      const x = await run(i);
      if (i > 0) r.samples.push(x);
    }
  } catch (e) {
    r.error = String((e as Error).message).split("\n")[0];
  } finally {
    await teardown?.().catch((e) => (r.error ??= `teardown: ${(e as Error).message}`));
  }
  console.error(`${id} ${r.error ? `error: ${r.error}` : "done"}`);
}

const admin = await login("admin");
const manager = await login("manager");
const finance = await login("finance");
const me = await (await admin.ctx.request.get(`${base}/api/me`)).json();

// B1, B2: navigation to the entry screen and to generic screens.
await scenario("B1", "click → entry screen displayed", 100, admin, () => open(admin.page, "entry"));
for (const [i, screen] of (["activities", "invoicing", "locks", "project"] as const).entries())
  await scenario(`B2${"abcd"[i]}`, `click → generic screen ${screen}, first page`, 120, admin, () => open(admin.page, screen));

// B3: Enter → data-saved on the entry screen, create then update, in the perf week.
const folders = await view(admin, "folder.lookup", { filter: [{ col: "entry_open", op: "eq", value: true }], limit: 50 });
let folder = folders[0];
let task: Record<string, unknown> = {};
for (const f of folders) {
  const t = (await view(admin, "folder_task_allowed", { params: { folder_id: f.id } })).find((x) => x.billing === "always");
  if (t) {
    [folder, task] = [f, t];
    break;
  }
}
const perfActivities = async () =>
  (await view(admin, "timesheet", { params: { user_id: me.id, date_from: perfDay(0), date_to: perfDay(6) }, limit: 5000 })).map((r) => r.id as number);
const clearPerfWeek = async () => {
  const ids = await perfActivities();
  if (ids.length) await call(admin, "activity.delete", { ids });
};
async function saveLine(page: Page, uid: string): Promise<Sample> {
  await page.evaluate((uid) => {
    const w = window as unknown as Record<string, unknown>;
    const el = document.querySelector(`[data-uid="${uid}"]`)!;
    const saved = el.getAttribute("data-saved");
    let t0 = 0;
    addEventListener("keydown", () => (t0 = performance.now()), { capture: true, once: true });
    w.__saved = (w.__until as (c: () => boolean) => Promise<number>)(() => document.querySelector(`[data-uid="${uid}"]`)?.getAttribute("data-saved") !== saved).then(
      async (t1) => (await new Promise((r) => setTimeout(r, 30)), { ms: t1 - t0, requests: (w.__requests as (t: number) => Req[])(t0) }),
    );
  }, uid);
  await page.keyboard.press("Enter");
  return page.evaluate(() => (window as unknown as { __saved: Promise<Sample> }).__saved);
}
const focusedLine = (page: Page) =>
  page.evaluate(() => {
    const a = document.activeElement!;
    return { uid: a.closest<HTMLElement>("[data-uid]")?.dataset.uid, row: a.closest<HTMLElement>("[data-uid]")?.dataset.row, col: a.closest<HTMLElement>("[data-col]")?.dataset.col };
  });
async function entryOnPerfDay(page: Page) {
  await away(page);
  await page.evaluate(() => (location.hash = "#/entry"));
  await page.waitForSelector('[data-screen="entry"][data-fresh]');
  await page.locator(".strip .day").first().waitFor();
  for (let i = 0; i < 400 && (await page.locator(".strip .day[aria-pressed=true]").getAttribute("data-date"))! < perfDay(0); i++) await page.keyboard.press("Alt+ArrowDown");
  for (let i = 0; i < 7 && (await page.locator(".strip .day[aria-pressed=true]").getAttribute("data-date")) !== perfDay(0); i++) await page.keyboard.press("Alt+ArrowLeft");
}
const updates: Sample[] = [];
await scenario(
  "B3",
  "Enter → activity saved and confirmed (create; update in B3u)",
  50,
  admin,
  async (i) => {
    const page = admin.page;
    if (i % 10 === 0) await entryOnPerfDay(page);
    for (let k = 0; k < 50 && (await focusedLine(page)).row !== "new"; k++) await page.waitForTimeout(20);
    await page.keyboard.type(String(8 + (i % 10)));
    await page.keyboard.press("Tab");
    await page.keyboard.type("0:30");
    await page.keyboard.press("Tab");
    await page.keyboard.type(String(folder.label).split(" / ").pop()!);
    await page.locator(".popover .option").first().waitFor();
    await page.keyboard.press("Tab");
    await page.keyboard.type(String(task.name));
    await page.locator(".popover .option", { hasText: String(task.name) }).first().waitFor();
    await page.keyboard.press("Tab");
    await page.keyboard.type(`perf ${i}`);
    const { uid } = await focusedLine(page);
    const create = await saveLine(page, uid!);
    // update: the duration of the line just saved
    await page.locator(`[data-uid="${uid}"] [data-col="duration_min"] input`).click();
    await page.keyboard.press("Control+a");
    await page.keyboard.type(i % 2 ? "0:45" : "0:40");
    const update = await saveLine(page, uid!);
    if (i > 0) updates.push(update);
    await page.locator(`[data-uid="${uid}"]`).locator("xpath=following-sibling::*[@data-row='new'][1]").locator("input").first().focus().catch(() => {});
    return create;
  },
  clearPerfWeek,
  clearPerfWeek,
);
if (results.at(-1)?.id === "B3") results.push({ id: "B3u", scenario: "Enter → activity update saved and confirmed", budget: 50, user: "admin", samples: updates });

// B4: bulk validation of 100 activities, confirmed and list refreshed.
let toValidate: number[] = [];
await scenario(
  "B4",
  "validation of 100 activities → confirmed, list refreshed",
  250,
  admin,
  async () => {
    const page = admin.page;
    await away(page);
    await page.evaluate((h) => (location.hash = h), hash("validation", { wrap: { filter: [{ action: "validatable" }, { col: "id", op: "in", value: toValidate }] } }));
    await page.waitForSelector('[data-screen="validation"][data-fresh]');
    await page.locator(".grid .row[data-id]").first().waitFor();
    await page.locator(".hrow .checkbox").click();
    await page.waitForFunction(() => document.querySelector(".status")?.textContent?.includes("100 sélectionnée"));
    const s = await measure(page, `document.querySelector('[data-command="activity.validate"]').click()`, '[data-screen="validation"] .empty-state');
    await call(admin, "activity.unvalidate", { ids: toValidate });
    return s;
  },
  async () => {
    toValidate = (await view(admin, "activity_team", { params: { date_from: daysAgo(90), date_to: iso(today) }, filter: [{ action: "validatable" }, { col: "not_invoiced", op: "eq", value: true }], limit: 100 })).map((r) => r.id as number);
    if (toValidate.length < 100) throw new Error(`only ${toValidate.length} activities to validate`);
  },
);

// B5, B6: balances; B7, B8: profitability with cost; B9: activities grid, first page and a deep page.
await scenario("B5", "balances, team (manager), current year", 150, manager, () => open(manager.page, "balances"));
await scenario("B6", "balances, all users (admin), whole history", 500, admin, () =>
  open(admin.page, "balances", { params: { date_from: "2000-01-01", date_to: iso(today) } }));
await scenario("B7", "profitability, 12 months, all projects (finance)", 400, finance, () =>
  open(finance.page, "profitability", { params: { date_from: daysAgo(365), date_to: iso(today) } }));
await scenario("B8", "profitability, whole history (finance)", 900, finance, () =>
  open(finance.page, "profitability", { params: { date_from: "2000-01-01", date_to: iso(today) } }));
await scenario("B9", "activities grid, 1 month, all users, first page", 120, admin, () =>
  open(admin.page, "activities", { params: { date_from: daysAgo(30), date_to: iso(today) } }));
await scenario("B9d", "activities grid, 1 month, deep scroll (middle)", 120, admin, async () => {
  await open(admin.page, "activities", { params: { date_from: daysAgo(30), date_to: iso(today) } });
  return measure(
    admin.page,
    `const v = document.querySelector(".viewport"); v.scrollTop = Math.floor(v.scrollHeight / 2 / 24) * 24;`,
    ".viewport",
    `const top = el.getBoundingClientRect().top; return !document.querySelector(".row.loading") && [...el.querySelectorAll(".row[data-id]")].some((r) => Math.abs(r.getBoundingClientRect().top - top - 56) < 200);`,
  );
});

// B13: back to a visited screen, displayed from the client cache; B14: a reload, from its navigation start to the
// shell and the screen displayed (from the cache kept by the tab).
const month30 = { params: { date_from: daysAgo(30), date_to: iso(today) } };
await scenario("B13", "return to a visited screen → displayed", 50, admin, async () => {
  await away(admin.page);
  return measure(admin.page, link(hash("activities", month30)), '[data-screen="activities"][data-ready]');
});
await scenario("B14", "reload the page → shell and screen displayed", 500, admin, async () => {
  const page = admin.page;
  await page.evaluate((h) => (location.hash = h), hash("activities", month30));
  await page.waitForSelector('[data-screen="activities"][data-fresh]');
  await page.reload();
  await page.waitForSelector('[data-screen="activities"][data-ready]');
  const ms = await page.evaluate(() => (window as unknown as { __shown: number }).__shown);
  await page.waitForSelector('[data-screen="activities"][data-fresh]');
  return { ms, requests: [] };
});

// B10: invoice run for one month: first progress and end; then cancel at first progress and rerun.
const month = (() => {
  const a = new Date(today.getFullYear(), today.getMonth() - 1, 1);
  return [iso(a), iso(new Date(today.getFullYear(), today.getMonth(), 0))];
})();
let maxInvoice = 0;
const deleteNewInvoices = async () => {
  const ids = (await view(admin, "invoice", { filter: [{ col: "id", op: "gt", value: maxInvoice }], limit: 5000 })).map((r) => r.id as number);
  if (ids.length) await call(admin, "invoice.delete", { ids });
};
async function startRun(page: Page) {
  await page.locator('[data-command="invoice.run"]').click();
  for (const [f, d] of [["date_from", month[0]], ["date_to", month[1]]]) {
    await page.locator(`#run-${f}`).locator('[data-segment="day"]').click();
    await page.keyboard.type(d.split("-").reverse().join(""));
  }
}
const progressed = `const n = el.querySelector(".num")?.textContent ?? ""; return !n.startsWith("0 ") && !n.startsWith("0 ") && /^\\d/.test(n);`;
const runEnds: Sample[] = [];
const rerunEnds: Sample[] = [];
await scenario(
  "B10",
  "invoice run, 1 month: first progress (end in B10e, cancel and rerun in B10r)",
  300,
  admin,
  async (i) => {
    const page = admin.page;
    await away(page);
    await page.evaluate((h) => (location.hash = h), hash("invoicing"));
    await page.waitForSelector('[data-screen="invoicing"][data-fresh]');
    await startRun(page);
    const start = `[...document.querySelectorAll(".dialog-footer button")].at(-1).click()`;
    const first = await measure(page, start, ".run[data-run-state]", progressed);
    if (i % 2 === 0) {
      // from the start of the measure of the first progress
      const end = await page.evaluate(async () => {
        const w = window as unknown as { __until: (c: () => boolean) => Promise<number>; __t0: number };
        return (await w.__until(() => !!document.querySelector('.run[data-run-state="done"]'))) - w.__t0;
      });
      if (i > 0) runEnds.push({ ms: end, requests: [] });
    } else {
      // a short run may end before the cancel: the rerun then finds nothing left
      await page.evaluate(() => [...document.querySelectorAll<HTMLButtonElement>(".dialog-footer button")].find((b) => b.textContent?.includes("Annuler le traitement"))?.click());
      await page.locator('.run[data-run-state="cancelled"], .run[data-run-state="done"]').waitFor();
      await page.locator(".dialog-footer button").last().click();
      await startRun(page);
      const rerun = await measure(page, start, '.run[data-run-state="done"]');
      if (i > 0) rerunEnds.push(rerun);
    }
    await page.locator(".dialog-footer button").last().click();
    await deleteNewInvoices();
    return first;
  },
  async () => {
    maxInvoice = ((await view(admin, "invoice", { agg: [{ col: "id", fn: "max" }] }))[0].max_id as number) ?? 0;
  },
  deleteNewInvoices,
);
if (results.at(-1)?.id === "B10") {
  results.push({ id: "B10e", scenario: "invoice run, 1 month: end", budget: 5000, user: "admin", samples: runEnds });
  results.push({ id: "B10r", scenario: "invoice run, cancelled then rerun: end of the rerun", budget: 5000, user: "admin", samples: rerunEnds });
}

// B11: print → PDF displayed, on an invoiced invoice whose PDF is removed before each run.
let printed = 0;
await scenario(
  "B11",
  "print → PDF displayed",
  1500,
  admin,
  async () => {
    const page = admin.page;
    await call(admin, "invoice.attach_pdf", { ids: [printed], pdf_file_id: null });
    await away(page);
    await page.evaluate((h) => (location.hash = h), hash("invoicing", { wrap: { filter: [{ col: "id", op: "eq", value: printed }] } }));
    await page.waitForSelector('[data-screen="invoicing"][data-fresh]');
    await page.locator(".grid .row[data-id]").first().dblclick();
    await page.getByRole("tab", { name: "PDF" }).click();
    await page.waitForSelector('[data-screen="invoice_pdf"][data-fresh]');
    await page.locator(".grid .row[data-id]").first().click();
    return measure(page, `document.querySelector('[data-command="invoice.print"]').click()`, '[data-screen="invoice_pdf"] iframe');
  },
  async () => {
    printed = (await view(admin, "invoice", { filter: [{ col: "state", op: "eq", value: "invoiced" }], sort: [{ col: "id", dir: "desc" }], limit: 1 }))[0].id as number;
  },
);

// B12: a commit in session A shows in session B (the activities grid on the perf week).
const other = await login("admin");
let liveId = 0;
await scenario(
  "B12",
  "commit in session A → row updated in session B",
  300,
  admin,
  async (i) => {
    const text = `live ${Date.now()} ${i}`;
    await other.page.evaluate((text) => {
      const w = window as unknown as Record<string, unknown>;
      w.__seen = (w.__until as (c: () => boolean) => Promise<number>)(() => !!document.querySelector(".grid")?.textContent?.includes(text)).then(
        (t) => performance.timeOrigin + t,
      );
    }, text);
    const t0 = await admin.page.evaluate(async ([id, text]) => {
      const t = performance.timeOrigin + performance.now();
      await fetch("/api/c/activity.update", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ ids: [id], description: text }) });
      return t;
    }, [liveId, text] as const);
    const t1 = await other.page.evaluate(() => (window as unknown as { __seen: Promise<number> }).__seen);
    return { ms: t1 - t0, requests: [] };
  },
  async () => {
    liveId = (await call(admin, "activity.create", { user_id: me.id, date: perfDay(1), start_tod: 480, duration_min: 30, folder_id: folder.id, task_id: task.id })).ids[0];
    await other.page.evaluate((h) => (location.hash = h), hash("activities", { params: { date_from: perfDay(1), date_to: perfDay(1) } }));
    await other.page.waitForSelector('[data-screen="activities"][data-fresh]');
  },
  clearPerfWeek,
);

await browser.close();

// Report: p50/p95 per scenario; Server-Timing per request name; S1 round trips, S2/S3 server times.
const pct = (xs: number[], p: number) => {
  if (!xs.length) return NaN;
  const s = [...xs].sort((a, b) => a - b);
  return s[Math.min(s.length - 1, Math.ceil((p / 100) * s.length) - 1)];
};
const r1 = (x: number) => Math.round(x * 10) / 10;
const rows = results.map((r) => {
  const ms = r.samples.map((s) => s.ms);
  const reqs = r.samples.flatMap((s) => s.requests);
  const byName = Object.fromEntries(
    [...new Set(reqs.map((q) => q.name))].map((n) => {
      const q = reqs.filter((x) => x.name === n);
      return [n, { count: q.length, db_p95: r1(pct(q.map((x) => x.db), 95)), app_p95: r1(pct(q.map((x) => x.app), 95)), rt_max: Math.max(...q.map((x) => x.rt)) }];
    }),
  );
  const p95 = pct(ms, 95);
  return { ...r, p50: r1(pct(ms, 50)), p95: r1(p95), pass: !r.error && ms.length > 0 && p95 <= r.budget, requests: byName };
});
const all = results.flatMap((r) => r.samples.flatMap((s) => s.requests));
const maxRt = (f: (q: Req) => boolean) => Math.max(0, ...all.filter(f).map((q) => q.rt));
const appP95 = (f: (q: Req) => boolean) => r1(pct(all.filter(f).map((q) => q.app), 95));
const jobs = /^c\/(expense\.attach_receipt|invoice\.print|invoice\.export|connection\.pull|delivery\.retry)$/;
const server = [
  { id: "S1v", what: "DB round trips, view (max)", budget: 2, value: maxRt((q) => q.name.startsWith("v/")) },
  { id: "S1c", what: "DB round trips, command (max)", budget: 4, value: maxRt((q) => q.name.startsWith("c/") && !jobs.test(q.name) && q.name !== "c/invoice.run") },
  { id: "S1j", what: "DB round trips, command enqueuing jobs (max)", budget: 5, value: maxRt((q) => jobs.test(q.name) || q.name === "c/invoice.run") },
  { id: "S2", what: "server, activity command (p95 ms)", budget: 15, value: appP95((q) => /^c\/activity\./.test(q.name)) },
  { id: "S3t", what: "server, timesheet (p95 ms)", budget: 5, value: appP95((q) => q.name === "v/timesheet") },
  { id: "S3g", what: "server, grid page activity_grid, page request (p95 ms)", budget: 30, value: appP95((q) => q.name === "v/activity_grid") },
].map((s) => ({ ...s, pass: Number.isFinite(s.value) && s.value <= s.budget }));

writeFileSync(out, JSON.stringify({ at: new Date().toISOString(), base, runs: RUNS, scenarios: rows, server }, null, 2));
const line = (c: unknown[]) => `| ${c.join(" | ")} |`;
console.log(line(["#", "scenario", "user", "budget ms", "p50 ms", "p95 ms", "pass", "requests: db p95 ms / server p95 ms / rt max"]));
console.log(line(["---", "---", "---", "---:", "---:", "---:", "---", "---"]));
for (const r of rows)
  console.log(line([r.id, r.scenario, r.user, r.budget, r.p50, r.p95, r.error ? `error: ${r.error}` : r.pass ? "pass" : "FAIL",
    Object.entries(r.requests).map(([n, q]) => `${n} ${q.db_p95}/${q.app_p95}/${q.rt_max}`).join("; ")]));
console.log();
console.log(line(["#", "server", "budget", "value", "pass"]));
console.log(line(["---", "---", "---:", "---:", "---"]));
for (const s of server) console.log(line([s.id, s.what, s.budget, s.value, s.pass ? "pass" : "FAIL"]));
console.log(`\n${out}`);

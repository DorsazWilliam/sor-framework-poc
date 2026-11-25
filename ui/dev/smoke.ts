// Smoke test against the running stack (`docker compose up`): `npm run smoke` [-- step-name-filter].
// SMOKE_URL (default http://localhost:8080) may point at the Vite dev server, which proxies to the stack.
// Everything it creates is deleted again; dates are far in the future so seeded data is not touched.
import { chromium, type BrowserContext, type Page } from "playwright";

const base = process.env.SMOKE_URL ?? "http://localhost:8080";
const only = process.argv[2];
const shots = new URL("./screenshots/", import.meta.url).pathname.replace(/^\/(\w:)/, "$1");
const browser = await chromium.launch();
const errors: string[] = [];

async function login(user: string, viewport = { width: 1440, height: 900 }) {
  const ctx = await browser.newContext({ viewport, colorScheme: "light" });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => errors.push(`${user}: ${e.message}`));
  page.on("console", (m) => m.type() === "error" && !/\b(401|403|409|422)\b/.test(m.text()) && errors.push(`${user}: ${m.text()}`));
  await page.goto(`${base}/`);
  await page.fill('input[name="username"]', user);
  await page.click('input[type="submit"]');
  await page.waitForURL((u) => u.pathname === "/" && u.port === "8080");
  return { ctx, page };
}

// A failed step is reported (with a screenshot in SMOKE_FAIL, if set) and the next one runs.
const failed: string[] = [];
async function step(name: string, fn: () => Promise<unknown>) {
  if (only && !only.split("|").some((o) => name.includes(o))) return;
  const t0 = performance.now();
  try {
    await fn();
    console.log(`ok   ${name} (${Math.round(performance.now() - t0)} ms)`);
  } catch (e) {
    failed.push(name);
    console.log(`FAIL ${name}: ${String((e as Error).message).split("\n")[0]}`);
    if (process.env.SMOKE_FAIL) await page.screenshot({ path: `${process.env.SMOKE_FAIL}/fail-${failed.length}.png` }).catch(() => {});
    await page.keyboard.press("Escape").catch(() => {});
  }
}
function check(cond: unknown, what: string) {
  if (!cond) throw new Error(`failed: ${what}`);
}
async function until(page: Page, fn: () => Promise<boolean>, what: string, ms = 10000) {
  for (const end = Date.now() + ms; Date.now() < end; await page.waitForTimeout(50)) if (await fn()) return;
  throw new Error(`timeout: ${what}`);
}

// API through the context's session cookie.
async function post<T = Record<string, unknown>[]>(ctx: BrowserContext, path: string, body: unknown): Promise<T> {
  const r = await ctx.request.post(`${base}${path}`, { data: body });
  if (!r.ok()) throw new Error(`${path}: ${r.status()} ${await r.text()}`);
  return r.json();
}
const view = (ctx: BrowserContext, v: string, body: object) => post(ctx, `/api/v/${v}`, body);
const call = (ctx: BrowserContext, c: string, body: object) => post<{ ids: number[] }>(ctx, `/api/c/${c}`, body);

const iso = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
const today = new Date();
const monday = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate() - ((d.getDay() + 6) % 7));
// The test week: a future week never seeded, different on each run (an exported invoice cannot be removed).
const weeks = 8 + (Math.floor(Date.now() / 60000) % 40);
const testMonday = monday(new Date(today.getFullYear(), today.getMonth(), today.getDate() + 7 * weeks));
const testDay = (n: number) => iso(new Date(testMonday.getFullYear(), testMonday.getMonth(), testMonday.getDate() + n));

const ready = (p: Page, id: string) => p.waitForSelector(`[data-screen="${id}"][data-ready]`);
const open = async (p: Page, screen: string, state?: object) => {
  await p.goto(`${base}/#/${screen}${state ? `?s=${encodeURIComponent(JSON.stringify(state))}` : ""}`);
  await ready(p, screen);
};
// A command of the screen's bar; one that does not fit is in the bar's "more" popover.
async function command(p: Page, name: string) {
  const b = p.locator(`.commandbar [data-command="${name}"]`);
  if (await b.isVisible()) return b.click();
  await p.locator(".commandbar > button[aria-haspopup]").click();
  await p.locator(`.more-commands [data-command="${name}"]`).click();
}
const toast = (p: Page, text: string | RegExp) => p.locator(".toast", { hasText: text }).first().waitFor();
const closeToasts = async (p: Page) => {
  for (const b of await p.locator(".toast button").all()) await b.click().catch(() => {});
};
const status = (p: Page) => p.locator(".status span").first().innerText();
const colIndex = async (p: Page, label: string) =>
  (await p.locator(".hrow .hbtn").allInnerTexts()).findIndex((t) => t.trim().replace(/\s*[↑↓]\s*\d*$/, "") === label);
const cell = (p: Page, row: number, col: number) => p.locator(".row[data-id]").nth(row).locator(".cell").nth(col + 1).innerText();

// A receipt image drawn in the page: the OCR job reads its total.
async function receipt(p: Page): Promise<Buffer> {
  const data = await p.evaluate(() => {
    const c = document.createElement("canvas");
    c.width = 600;
    c.height = 520;
    const g = c.getContext("2d")!;
    g.fillStyle = "#fff";
    g.fillRect(0, 0, c.width, c.height);
    g.fillStyle = "#000";
    g.font = "32px monospace";
    const lines = ["CAFE DE LA GARE", "", "Menu du jour     32.50", "Eau minerale      6.80", "Cafe              6.50", "", "Total CHF        45.80", "TVA 8.1%          3.43"];
    lines.forEach((l, i) => g.fillText(l, 40, 60 + i * 52));
    return c.toDataURL("image/png").split(",")[1];
  });
  return Buffer.from(data, "base64");
}

const admin = await login("admin");
const { page } = admin;

// Test data: a folder open to entry with a task billed "always", and its label as typed in the entry screen.
const folders = await view(admin.ctx, "folder.lookup", { filter: [{ col: "entry_open", op: "eq", value: true }], limit: 50 });
let folder = folders[0];
let task: Record<string, unknown> = {};
for (const f of folders) {
  const tasks = await view(admin.ctx, "folder_task_allowed", { params: { folder_id: f.id } });
  const t = tasks.find((x) => x.billing === "always" && x.kind === "service");
  if (t) {
    folder = f;
    task = t;
    break;
  }
}
const [projectLabel, folderName] = String(folder.label).split(" / ");
const folderQuery = `${projectLabel.split(" ").pop()} ${folderName}`;
// The activity the generic screens work on (expenses, drawer, live, invoicing), in the test week.
const activityId = (await call(admin.ctx, "activity.create", {
  user_id: (await (await admin.ctx.request.get(`${base}/api/me`)).json()).id,
  date: testDay(0), start_tod: 480, duration_min: 60, folder_id: folder.id, task_id: task.id, description: "Smoke frais",
})).ids[0];

// ---- 1 entry ----
const kb = page.keyboard;
const focused = () =>
  page.evaluate(() => {
    const a = document.activeElement as HTMLElement;
    const line = a.closest<HTMLElement>("[data-uid]");
    return { inGrid: !!a.closest(".lines"), col: a.closest<HTMLElement>("[data-col]")?.dataset.col, uid: line?.dataset.uid, row: line?.dataset.row, saved: line?.dataset.saved };
  });
const pressed = () => page.locator(".strip .day[aria-pressed=true]").getAttribute("data-date");
const lines = () => page.locator('.line:not([data-row="new"])');

async function typeLine(start: string, duration: string, description: string) {
  await until(page, async () => (await focused()).col === "start_tod" && (await focused()).row === "new", "on the new line");
  await kb.type(start);
  await kb.press("Tab");
  await kb.type(duration);
  await kb.press("Tab");
  await kb.type(folderQuery);
  await page.locator(".popover .option", { hasText: folderName }).first().waitFor();
  await kb.press("Tab");
  await until(page, async () => (await focused()).col === "task_id", "on the task");
  await kb.type(String(task.name).slice(0, 5));
  await page.locator(".popover .option", { hasText: String(task.name) }).first().waitFor();
  await kb.press("Tab");
  if ((await focused()).col === "billed_min") await kb.press("Tab");
  await kb.type(description);
  await kb.press("Enter");
}

async function gotoDay(date: string) {
  await until(page, async () => (await focused()).inGrid, "focus in the grid");
  for (let i = 0; i < 60 && (await pressed())! < date; i++) await kb.press((await pressed())! <= iso(new Date(Date.parse(date) - 7 * 864e5)) ? "Alt+ArrowDown" : "Alt+ArrowRight");
  for (let i = 0; i < 30 && (await pressed())! > date; i++) await kb.press("Alt+ArrowLeft");
  await until(page, async () => (await pressed()) === date && (await focused()).row === "new", `on ${date}`);
}

let lockId = 0;
await step("1 entry: keyboard create, update, refused save on a locked day, duplicate, delete", async () => {
  await open(page, "entry");
  await gotoDay(testDay(1));
  const n = await lines().count();
  await typeLine("9", "1.5", "Smoke analyse");
  await until(page, async () => (await lines().count()) === n + 1 && (await lines().first().getAttribute("data-saved")) === "1", "line created");
  await typeLine("", "45m", "Smoke revue");
  await until(page, async () => (await lines().count()) === n + 2, "second line created");
  // update: the duration of the first line
  const first = lines().first();
  await first.locator('[data-col="duration_min"] input').click();
  await kb.press("Control+a");
  await kb.type("2h");
  await kb.press("Enter");
  await page.locator(`[data-uid="${await first.getAttribute("data-uid")}"][data-saved="2"]`).waitFor();
  await until(page, async () => (await page.locator(".totals span").first().innerText()).includes("2:45"), "day total 2:45");
  // a lock on the test Wednesday refuses the save with the condition's label
  lockId = (await call(admin.ctx, "period_lock.create", { company_id: 1, date_from: testDay(2), date_to: testDay(2) })).ids[0];
  await gotoDay(testDay(2));
  await page.locator(".strip .day[aria-pressed=true] .mark").first().waitFor();
  await typeLine("10", "1", "Smoke refusée");
  await page.locator(".reasons", { hasText: "Date hors période verrouillée" }).waitFor();
  await kb.press("Escape");
  // duplicate (Ctrl+D) and delete (Ctrl+Delete) on the first day
  await gotoDay(testDay(1));
  const m = await lines().count();
  await first.locator('[data-col="description"] input').click();
  await kb.press("Control+d");
  await until(page, async () => (await focused()).row === "new", "on the duplicate");
  await kb.type("14");
  await kb.press("Enter");
  await until(page, async () => (await lines().count()) === m + 1, "duplicated");
  for (let i = m + 1; i > n; i--) {
    await lines().last().locator('[data-col="description"] input').click();
    await kb.press("Control+Delete");
    await until(page, async () => (await lines().count()) === i - 1, "deleted");
  }
});

await step("1 entry: week mode, user picker", async () => {
  await page.locator(".seg button").nth(1).click();
  await page.locator(".dayhead").nth(6).waitFor();
  check((await page.locator("#entry-user").count()) === 1, "user picker for a scope with several users");
  await page.locator(".seg button").nth(0).click();
});

await step("2 expenses: create a line, upload a receipt, amount pre-filled live by OCR", async () => {
  await open(page, "activities", { params: { date_from: testDay(0), date_to: testDay(0) }, wrap: { filter: [{ col: "id", op: "eq", value: activityId }] } });
  await page.locator(".row[data-id]").first().dblclick();
  await page.getByRole("tab", { name: "Frais" }).click();
  const tab = page.locator('.drawer [data-screen="activity_expense"][data-ready]');
  await tab.waitFor();
  await tab.locator('[data-command="expense.create"]').click();
  await page.locator(".dialog #f-expense_type_id").click();
  await kb.type("Repas");
  await page.locator(".popover .option", { hasText: "Repas" }).first().click();
  await page.locator(".dialog").getByRole("button", { name: "Créer" }).click();
  await tab.locator(".row[data-id]").first().waitFor();
  await tab.locator(".row[data-id]").first().click();
  await tab.locator('[data-command="expense.attach_receipt"]').click();
  await page.locator('.dialog input[type="file"]').setInputFiles({ name: "ticket.png", mimeType: "image/png", buffer: await receipt(page) });
  await page.locator(".dialog img").waitFor();
  await page.locator(".dialog-footer").getByRole("button", { name: "Exécuter" }).click();
  await tab.locator(".row[data-id]", { hasText: "45.80" }).first().waitFor({ timeout: 30000 });
});

const grid = (p: Page) => p.locator(".row[data-id]");
const typeDate = async (p: Page, id: string, d: string) => {
  await p.locator(`#${id}`).locator('[data-segment="day"]').click();
  await p.keyboard.type(d.split("-").reverse().join(""));
};

await step("4 activities: sort (number, ref by label), filter, group with totals, drill-down", async () => {
  await open(page, "activities");
  const all = await status(page);
  const d = await colIndex(page, "Durée");
  await page.locator(".hrow .hbtn").nth(d).click();
  const minutes = (s: string) => +s.split(":")[0] * 60 + +s.split(":")[1];
  await until(page, async () => {
    const ds = await Promise.all([0, 1, 2, 3, 4, 5].map(async (i) => minutes(await cell(page, i, d))));
    return (await page.locator(".hcell[aria-sort=ascending]").count()) === 1 && ds.every((x, i) => !i || ds[i - 1] <= x);
  }, "sorted by duration");
  const u = await colIndex(page, "Utilisateur");
  await page.locator(".hrow .hbtn").nth(u).click();
  await until(page, async () => {
    const names = await Promise.all([0, 1, 2, 3, 4, 5].map((i) => cell(page, i, u)));
    return names.every((n, i) => !i || names[i - 1].localeCompare(n, "fr") <= 0) && names[0].startsWith("A");
  }, "sorted by user label");
  const desc = await colIndex(page, "Description");
  const input = page.locator(".frow .fcell").nth(desc).locator("input");
  await input.fill("Rapport");
  await input.press("Enter");
  await until(page, async () => (await status(page)) !== all && (await cell(page, 0, desc)) === "Rapport", "filtered");
  await page.getByRole("button", { name: "Effacer les filtres" }).click();
  await until(page, async () => (await status(page)) === all, "filter cleared");
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Utilisateur", exact: true }).first().click();
  await page.locator(".row.group").first().waitFor();
  check((await page.locator(".total").innerText()).includes(all.split(" ")[0]), "total count");
  await page.locator(".row.group").first().click();
  await grid(page).first().waitFor();
});

await step("4 activities: saved views save, load, delete", async () => {
  const name = `Smoke ${weeks}`;
  await page.getByRole("button", { name: /Vues/ }).click();
  await page.getByRole("menuitem", { name: "Enregistrer la vue sous…" }).click();
  await page.locator("#sv-name").fill(name);
  await page.locator(".dialog-footer").getByRole("button", { name: "Enregistrer" }).click();
  await page.getByRole("button", { name }).waitFor();
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Aucun" }).click();
  await until(page, async () => !(await page.locator(".row.group").count()), "ungrouped");
  await page.getByRole("button", { name }).click();
  await page.getByRole("menuitem", { name }).click();
  await page.locator(".row.group").first().waitFor();
  await page.getByRole("button", { name }).click();
  await page.getByRole("menuitem", { name: "Supprimer la vue" }).click();
  await page.getByRole("button", { name: /^Vues/ }).waitFor();
});

await step("4/11 activities: drawer form update, classes, history", async () => {
  await open(page, "activities", { params: { date_from: testDay(0), date_to: testDay(0) }, wrap: { filter: [{ col: "id", op: "eq", value: activityId }] } });
  await grid(page).first().dblclick();
  const text = `Smoke modifiée ${weeks}`;
  await page.locator("#f-description").fill(text);
  await page.locator(".drawer").getByRole("button", { name: "Enregistrer" }).click();
  await toast(page, "Enregistré");
  await grid(page).filter({ hasText: text }).waitFor();
  await page.getByRole("tab", { name: "Historique" }).click();
  await page.locator(".history li", { hasText: text }).first().waitFor();
  await page.getByRole("tab", { name: "Classes d'heures" }).click();
  await page.locator('.drawer [data-screen="activity_class"][data-ready]').waitFor();
});

await step("12 live: a commit in one session updates another session", async () => {
  const other = await login("admin");
  await open(other.page, "activities", { params: { date_from: testDay(0), date_to: testDay(0) } });
  const text = `Smoke live ${Date.now() % 100000}`;
  await call(admin.ctx, "activity.update", { ids: [activityId], description: text });
  await grid(other.page).filter({ hasText: text }).waitFor({ timeout: 3000 });
  await other.ctx.close();
});

await step("cache: a return without requests, a change from another session, a reload painted from cache, logout, another user", async () => {
  const { ctx, page: p } = await login("admin");
  const first = await (await ctx.request.get(`${base}/api/me`)).json();
  // the landing screen first: the shell sends a session without a screen to its first one
  await p.locator("[data-screen][data-ready]").waitFor();
  await open(p, "locks");
  await p.locator('.live[data-state="live"]').waitFor();
  const a = { params: { date_from: testDay(0), date_to: testDay(0) } };
  const hash = (screen: string, state?: object) => `#/${screen}${state ? `?s=${encodeURIComponent(JSON.stringify(state))}` : ""}`;
  const visit = async (screen: string, state?: object) => {
    await p.evaluate((h) => (location.hash = h), hash(screen, state));
    await p.locator(`[data-screen="${screen}"][data-fresh]`).waitFor();
  };
  await visit("activities", a);
  await visit("locks");
  // back to the activities: shown at once (no skeleton, no empty state) and nothing read
  const requests: string[] = [];
  p.on("request", (r) => r.url().includes("/api/v/") && requests.push(r.url()));
  const flashed = await p.evaluate(async (h) => {
    let seen = false;
    const o = new MutationObserver(() => (seen ||= !!document.querySelector('[data-screen="activities"]:not([data-ready]), .row.loading, .empty-state')));
    o.observe(document, { subtree: true, childList: true, attributes: true });
    location.hash = h;
    while (!document.querySelector('[data-screen="activities"][data-fresh]')) await new Promise((r) => setTimeout(r, 10));
    o.disconnect();
    return seen;
  }, hash("activities", a));
  check(!flashed && !requests.length, `return shown at once without requests (${requests.length})`);
  // a change from another session while away: the activities show it on return
  await visit("locks");
  const text = `Smoke cache ${Date.now() % 100000}`;
  await call(admin.ctx, "activity.update", { ids: [activityId], description: text });
  await p.evaluate((h) => (location.hash = h), hash("activities", a));
  await grid(p).filter({ hasText: text }).waitFor({ timeout: 3000 });
  // a reload paints from the cache while every API response is held
  const api = (u: URL) => u.pathname.startsWith("/api/");
  await p.route(api, async (r) => {
    await new Promise((res) => setTimeout(res, 2000));
    await r.continue().catch(() => {});
  });
  await p.reload({ waitUntil: "commit" });
  await grid(p).filter({ hasText: text }).waitFor({ timeout: 1500 });
  await p.unroute(api);
  await p.locator('[data-screen="activities"][data-fresh]').waitFor();
  // logout empties the tab's cache; the next user sees nothing of the first one
  await p.route("**/auth/login", (r) => r.fulfill({ body: "" }));
  await p.getByTestId("user-menu").click();
  await p.getByRole("menuitem", { name: "Se déconnecter" }).click();
  await p.waitForURL((u) => u.pathname === "/auth/login");
  check(!(await p.evaluate(() => Object.keys(sessionStorage).length)), "sessionStorage empty after logout");
  await p.unroute("**/auth/login");
  await p.goto(`${base}/auth/login`);
  await p.fill('input[name="username"]', "user");
  await p.click('input[type="submit"]');
  const name = await p.getByTestId("user-menu").innerText();
  const me = await (await ctx.request.get(`${base}/api/me`)).json();
  const keys = await p.evaluate(() => Object.keys(sessionStorage));
  check(me.self !== first.self && name.includes(me.name) && !name.includes(first.name) && keys.every((k) => k === "sor:boot" || k.startsWith(`sor:v:${me.self}|`)), "only the second user's data");
  await ctx.close();
});

await step("3 validation: manager's list, bulk validate, live list", async () => {
  const mgr = await login("manager");
  const p = mgr.page;
  await open(p, "validation");
  const before = await status(p);
  await grid(p).nth(0).click();
  await grid(p).nth(2).click({ modifiers: ["Shift"] });
  const ids = (await p.locator(".row.selected").evaluateAll((rs) => rs.map((r) => +(r as HTMLElement).dataset.id!))) as number[];
  check(ids.length === 3, "three rows selected");
  await command(p, "activity.validate");
  await until(p, async () => (await status(p)) !== before, "list refreshed");
  await call(mgr.ctx, "activity.unvalidate", { ids });
  await until(p, async () => (await status(p)) === before, "back after unvalidate (live)");
  await mgr.ctx.close();
});

let invoiceId = 0;
await step("7 invoicing: run with live progress and cancel, rerun", async () => {
  await open(page, "invoicing");
  const [{ max_id }] = (await view(admin.ctx, "invoice", { agg: [{ col: "id", fn: "max" }] })) as { max_id: number }[];
  await command(page, "invoice.run");
  await typeDate(page, "run-date_from", "2000-01-01");
  await typeDate(page, "run-date_to", iso(today));
  await page.getByRole("button", { name: "Démarrer" }).click();
  await page.locator(".run[data-run-state]").waitFor();
  await page.getByRole("button", { name: "Annuler le traitement" }).click({ timeout: 2000 }).catch(() => {});
  await page.locator('[data-run-state="cancelled"], [data-run-state="done"]').waitFor({ timeout: 60000 });
  const cancelled = (await page.locator(".run").getAttribute("data-run-state")) === "cancelled";
  const created = await view(admin.ctx, "invoice", { filter: [{ col: "id", op: "gt", value: max_id }], limit: 5000 });
  if (created.length) await call(admin.ctx, "invoice.delete", { ids: created.map((r) => r.id as number) });
  check(cancelled && !created.length, "cancelled, nothing written");
  await page.locator(".dialog-footer").getByRole("button", { name: "Fermer" }).click();
  await command(page, "invoice.run");
  await typeDate(page, "run-date_from", testDay(0));
  await typeDate(page, "run-date_to", testDay(6));
  await page.getByRole("button", { name: "Démarrer" }).click();
  await page.locator('[data-run-state="done"]').waitFor({ timeout: 30000 });
  await page.locator(".dialog-footer").getByRole("button", { name: "Fermer" }).click();
  const runs = await view(admin.ctx, "invoice", { filter: [{ col: "date_from", op: "eq", value: testDay(0) }] });
  invoiceId = runs[0]?.id as number;
  check(runs.length === 1, "one invoice created by the run");
});

await step("7 invoicing: mark invoiced, back to draft, print (PDF tab), export with failures and retry", async () => {
  check(invoiceId, "an invoice from the run");
  await open(page, "invoicing", { wrap: { filter: [{ col: "id", op: "eq", value: invoiceId }] } });
  const row = grid(page).first();
  const state = () => row.innerText();
  await row.click();
  await command(page, "invoice.mark_invoiced");
  await until(page, async () => (await state()).includes("Facturée"), "invoiced");
  await command(page, "invoice.back_to_draft");
  await until(page, async () => (await state()).includes("En cours"), "back to draft");
  await command(page, "invoice.mark_invoiced");
  await until(page, async () => (await state()).includes("Facturée"), "invoiced again");
  await fetch("http://localhost:8070/admin/fail", { method: "POST", body: JSON.stringify({ on: true }) });
  await command(page, "invoice.export");
  await row.dblclick();
  await page.getByRole("tab", { name: "Exports" }).click();
  const exp = page.locator('.drawer [data-screen="invoice_export"]');
  await exp.locator(".row[data-id]", { hasText: "En échec" }).waitFor({ timeout: 30000 });
  await fetch("http://localhost:8070/admin/fail", { method: "POST", body: JSON.stringify({ on: false }) });
  await exp.locator(".row[data-id]").first().click();
  await exp.locator('[data-command="delivery.retry"]').click();
  await exp.locator(".row[data-id]", { hasText: "Exportée" }).waitFor({ timeout: 30000 });
});

await step("7 invoicing: print, the PDF tab displays it", async () => {
  check(invoiceId, "an invoice from the run");
  await open(page, "invoicing", { wrap: { filter: [{ col: "id", op: "eq", value: invoiceId }] } });
  await grid(page).first().dblclick();
  await page.getByRole("tab", { name: "PDF" }).click();
  await page.locator('.drawer [data-screen="invoice_pdf"][data-ready]').waitFor();
  await grid(page).first().click();
  await command(page, "invoice.print");
  await page.locator('.drawer [data-screen="invoice_pdf"] iframe').waitFor({ timeout: 15000 });
});

let lockRow = 0;
await step("8 locks: create and delete in the screen; an activity in the locked period is refused", async () => {
  await open(page, "locks");
  await command(page, "period_lock.create");
  await typeDate(page, "f-date_from", testDay(3));
  await typeDate(page, "f-date_to", testDay(4));
  await page.locator(".drawer").getByRole("button", { name: "Créer" }).click();
  await toast(page, "Enregistré");
  lockRow = (await view(admin.ctx, "period_lock", { filter: [{ col: "date_from", op: "eq", value: testDay(3) }] }))[0]?.id as number;
  const r = await call(admin.ctx, "activity.create", { user_id: 1, date: testDay(3), start_tod: 480, duration_min: 30, folder_id: folder.id, task_id: task.id }).catch((e: Error) => e);
  check(r instanceof Error && r.message.includes("not_locked"), "activity refused in the locked period");
  await remove("locks", "period_lock", lockRow);
  lockRow = 0;
});

// Master data: rows created through the drawer form, removed through the delete command.
async function createRow(screen: string, fill: () => Promise<void>) {
  await open(page, screen);
  await command(page, `${screen}.create`);
  await fill();
  await page.locator(".drawer").getByRole("button", { name: "Créer" }).click();
  await toast(page, "Enregistré");
  await closeToasts(page);
}
const text = (id: string, v: string) => page.locator(`#f-${id}`).fill(v);
async function pick(id: string, q: string, scope = page.locator("body")) {
  await scope.locator(`#f-${id}`).click();
  await kb.type(q);
  await page.locator(".popover .option", { hasText: q }).first().click();
}
const lastId = async (tbl: string) => (await view(admin.ctx, tbl, { sort: [{ col: "id", dir: "desc" }], limit: 1 }))[0].id as number;
async function remove(screen: string, tbl: string, id: number) {
  await open(page, screen, { wrap: { filter: [{ col: "id", op: "eq", value: id }] } });
  await grid(page).first().click();
  await command(page, `${tbl}.delete`);
  await page.locator(".dialog-footer").getByRole("button", { name: "Supprimer" }).click();
  await page.locator(".empty-state").waitFor();
}
async function child(screen: string, tbl: string, id: number, tab: string, childScreen: string) {
  await open(page, screen, { wrap: { filter: [{ col: "id", op: "eq", value: id }] } });
  await grid(page).first().dblclick();
  await page.getByRole("tab", { name: tab }).click();
  const c = page.locator(`.drawer [data-screen="${childScreen}"][data-ready]`);
  await c.waitFor();
  return c;
}

await step("9 master data: every screen opens", async () => {
  for (const s of ["company", "user", "user_group", "account", "project", "folder", "task", "user_task_rate", "work_period", "cost_rate", "public_holiday", "hour_class", "expense_type"]) await open(page, s);
});

await step("9 master data: task, project, folder with allowed tasks, archive, delete refused by a foreign key", async () => {
  const tag = `Smoke ${weeks}`;
  await createRow("task", () => text("name", `${tag} tâche`));
  const taskId = await lastId("task");
  const [account] = await view(admin.ctx, "account.lookup", { limit: 1 });
  await createRow("project", async () => {
    await text("name", `${tag} projet`);
    await text("external_id", `S${weeks}-${Date.now() % 100000}`);
    await pick("account_id", account.label as string);
  });
  const projectId = await lastId("project");
  await createRow("folder", async () => {
    await text("name", `${tag} dossier`);
    await pick("project_id", `${tag} projet`);
  });
  const folderId = await lastId("folder");
  const tasks = await child("folder", "folder", folderId, "Tâche permise", "folder_task");
  await tasks.locator('[data-command="folder_task.create"]').click();
  await pick("task_id", `${tag} tâche`, page.locator(".dialog"));
  await page.locator(".dialog").getByRole("button", { name: "Créer" }).click();
  await tasks.locator(".row[data-id]").first().waitFor();
  await open(page, "project", { wrap: { filter: [{ col: "id", op: "eq", value: projectId }] } });
  await grid(page).first().dblclick();
  await page.locator("#f-archived").click();
  await page.locator(".drawer").getByRole("button", { name: "Enregistrer" }).click();
  await toast(page, "Enregistré");
  await open(page, "task", { wrap: { filter: [{ col: "id", op: "eq", value: taskId }] } });
  await grid(page).first().click();
  await command(page, "task.delete");
  await page.locator(".dialog-footer").getByRole("button", { name: "Supprimer" }).click();
  await toast(page, "Cette tâche est permise dans des dossiers.");
  await page.locator(".dialog-footer").getByRole("button", { name: "Annuler" }).click();
  await remove("folder", "folder", folderId);
  await remove("project", "project", projectId);
  await remove("task", "task", taskId);
});

await step("9 master data: user and group membership, work period, holiday, hour class with slots, expense type", async () => {
  const tag = `Smoke ${weeks}`;
  await createRow("user", async () => {
    await text("name", `${tag} utilisateur`);
    await text("subject", `smoke-${weeks}`);
  });
  const userId = await lastId("user");
  const [group] = await view(admin.ctx, "user_group", { filter: [{ col: "name", op: "eq", value: "manager" }] });
  const members = await child("user_group", "user_group", group.id as number, "Membre", "user_group_member");
  await members.locator('[data-command="user_group_member.create"]').click();
  await pick("user_id", `${tag} utilisateur`, page.locator(".dialog"));
  await page.locator(".dialog").getByRole("button", { name: "Créer" }).click();
  await members.locator(".row[data-id]", { hasText: `${tag} utilisateur` }).waitFor();
  const [member] = await view(admin.ctx, "user_group_member", { filter: [{ col: "user_id", op: "eq", value: userId }] });
  await call(admin.ctx, "user_group_member.delete", { ids: [member.id as number] });
  await createRow("work_period", async () => {
    await pick("user_id", `${tag} utilisateur`);
    await typeDate(page, "f-valid_from", testDay(0));
    for (let d = 0; d < 7; d++) {
      await page.locator("#f-week_min input").nth(d).fill(d < 5 ? "8:00" : "0");
      await kb.press("Tab");
    }
    await text("vacation_days", "25");
  });
  await remove("work_period", "work_period", await lastId("work_period"));
  await remove("user", "user", userId);
  await createRow("public_holiday", async () => {
    await typeDate(page, "f-date", testDay(5));
    await text("label", `${tag} férié`);
  });
  await remove("public_holiday", "public_holiday", await lastId("public_holiday"));
  await createRow("hour_class", () => text("label", `${tag} classe`));
  const classId = await lastId("hour_class");
  await child("hour_class", "hour_class", classId, "Créneau", "hour_class_slot");
  await remove("hour_class", "hour_class", classId);
  await createRow("expense_type", () => text("name", `${tag} frais`));
  await remove("expense_type", "expense_type", await lastId("expense_type"));
});

await step("10 accounting: connection, manual pull, last pull time", async () => {
  await open(page, "accounting");
  const before = (await view(admin.ctx, "connection", {}))[0].pulled_at;
  await grid(page).first().click();
  await command(page, "connection.pull");
  await until(page, async () => (await view(admin.ctx, "connection", {}))[0].pulled_at !== before, "pulled", 30000);
});

await step("dashboard: every permitted tile drawn, a mark opens the linked screen filtered on its axis value", async () => {
  await open(page, "dashboard");
  const tiles = await page.locator(".tile").all();
  check(tiles.length === 10, "admin sees the ten tiles");
  for (const tile of tiles) {
    const drawn = (await tile.getAttribute("data-state")) === "shown" && (await tile.locator('.number, [role="graphics-symbol"]').count()) > 0;
    check(drawn, `tile ${await tile.getAttribute("data-tile")} drawn`);
  }
  await page.locator('[data-tile="tile.unbilled_by_project"] [role="graphics-symbol"]').first().click();
  await ready(page, "profitability");
  check(decodeURIComponent(page.url()).includes('{"col":"project_id","op":"eq"'), "filtered on the project");
  await until(page, async () => (await page.locator(".row.group").count()) === 1, "one project");
});

await step("users: manager, finance and user see their screens", async () => {
  const nav = async (user: string) => {
    const s = await login(user);
    await s.page.locator(".rail .item").first().waitFor();
    const items = await s.page.locator(".rail a.item").evaluateAll((a) => a.map((x) => (x as HTMLAnchorElement).hash.slice(2)));
    return { ...s, items };
  };
  const fin = await nav("finance");
  check(fin.items.includes("profitability"), "finance sees profitability");
  await open(fin.page, "profitability");
  const { lang } = await (await fin.ctx.request.get(`${base}/api/me`)).json();
  const cost = (await (await fin.ctx.request.get(`${base}/api/labels/${lang}`)).json())["c.cost_amount"];
  check((await fin.page.locator(".hrow").innerText()).includes(cost), "finance sees cost columns");
  await fin.ctx.close();
  const usr = await nav("user");
  check(!usr.items.includes("profitability") && !usr.items.includes("validation"), "user sees neither profitability nor validation");
  await open(usr.page, "entry");
  check(!(await usr.page.locator("#entry-user").count()), "no user picker for a single-user reach");
  await usr.ctx.close();
  const mgr = await nav("manager");
  check(mgr.items.includes("validation"), "manager sees validation");
  await open(mgr.page, "entry");
  check(await mgr.page.locator("#entry-user").count(), "user picker for a manager");
  await mgr.ctx.close();
});

await step("unknown user: a clear page, no loop", async () => {
  const s = await login("nobody");
  await s.page.locator(".boot-error h1").waitFor();
  await s.ctx.close();
});

// cleanup
if (invoiceId) {
  await call(admin.ctx, "invoice.back_to_draft", { ids: [invoiceId] }).catch(() => {});
  await call(admin.ctx, "invoice.delete", { ids: [invoiceId] }).catch(() => {});
}
if (activityId) {
  const ex = await view(admin.ctx, "expense_by_activity", { params: { activity_id: activityId } }).catch(() => []);
  if (ex.length) await call(admin.ctx, "expense.delete", { ids: ex.map((e) => e.id as number) }).catch(() => {});
  await call(admin.ctx, "activity.delete", { ids: [activityId] }).catch(() => {});
}
for (const id of [lockId, lockRow]) if (id) await call(admin.ctx, "period_lock.delete", { ids: [id] }).catch(() => {});

await browser.close();
if (errors.length) console.log("page errors:\n" + errors.join("\n"));
if (failed.length || errors.length) process.exit(1);
console.log("smoke passed");

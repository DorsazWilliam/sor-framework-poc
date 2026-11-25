// Smoke test against the dev server + mock: `npm run dev` then `npm run smoke`.
import { chromium, type Page } from "playwright";

const base = process.env.SMOKE_URL ?? "http://localhost:5173";
const shots = new URL("./screenshots/", import.meta.url).pathname.replace(/^\/(\w:)/, "$1");
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 1440, height: 860 }, colorScheme: "light" });
const errors: string[] = [];
page.on("pageerror", (e) => errors.push(e.message));
page.on("console", (m) => m.type() === "error" && !m.text().includes("409 (Conflict)") && errors.push(m.text()));

async function step(name: string, fn: () => Promise<unknown>) {
  const t0 = performance.now();
  await fn();
  console.log(`ok  ${name} (${Math.round(performance.now() - t0)} ms)`);
}
function check(cond: unknown, what: string) {
  if (!cond) throw new Error(`failed: ${what}`);
}
async function until(fn: () => Promise<boolean>, what: string) {
  for (let i = 0; i < 100; i++) if (await fn()) return;
  else await page.waitForTimeout(50);
  throw new Error(`timeout: ${what}`);
}
const status = () => page.locator(".status").innerText();
const ready = (p: Page, id: string) => p.waitForSelector(`[data-screen="${id}"][data-ready]`);
const firstCell = (col: number) => page.locator(".row[data-id]").first().locator(".cell").nth(col + 1).innerText();
const colIndex = async (label: string) => (await page.locator(".hrow .hbtn").allInnerTexts()).findIndex((t) => t.trim() === label);
const home = async () => {
  for (const b of await page.locator(".toast button").all()) await b.click();
  await page.locator(".viewport").evaluate((v) => (v.scrollLeft = 0));
  await page.locator(".row[data-id]").first().waitFor();
  await page.mouse.move(0, 0);
};
const toast = (text: string) => page.locator(".toast", { hasText: text }).first().waitFor();

await step("open activities (generic screen 1)", async () => {
  await page.goto(`${base}/#/activities`);
  await ready(page, "activities");
  check((await status()).includes("50'000"), "50'000 rows in status bar");
});

await step("sort by duration (server-side)", async () => {
  const d = await colIndex("Durée");
  await page.locator(".hrow .hbtn").nth(d).click();
  await until(async () => (await firstCell(d)) === "0:15", "shortest duration first");
});

await step("filter description contains 'Atelier'", async () => {
  const d = await colIndex("Description");
  const input = page.locator(".frow .fcell").nth(d).locator("input");
  await input.fill("Atelier");
  await input.press("Enter");
  await until(async () => !(await status()).includes("50'000") && (await firstCell(d)) === "Atelier client", "filtered rows");
  await page.getByRole("button", { name: "Effacer les filtres" }).click();
  await page.waitForFunction(() => document.querySelector(".status")?.textContent?.includes("50'000"));
});

await step("scroll 50k rows to the end", async () => {
  await page.locator(".viewport").evaluate((v) => (v.scrollTop = v.scrollHeight));
  await page.waitForFunction(() => {
    const ids = [...document.querySelectorAll(".row[data-id]")];
    const vp = document.querySelector(".viewport")!;
    return ids.length > 0 && vp.scrollTop > 1_000_000;
  });
  check((await page.locator(".grid").getAttribute("aria-rowcount")) === "50000", "aria-rowcount");
  await page.locator(".viewport").evaluate((v) => (v.scrollTop = 0));
});

await step("select rows: click, shift-range, ctrl, keyboard", async () => {
  const rows = page.locator(".row[data-id]");
  await rows.nth(0).click();
  await rows.nth(4).click({ modifiers: ["Shift"] });
  await rows.nth(7).click({ modifiers: ["Control"] });
  check((await status()).includes("6 sélectionnée"), "6 selected");
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Shift+ArrowDown");
  check((await status()).includes("2 sélectionnée"), "keyboard range");
});

await step("group by employee, drill down", async () => {
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Collaborateur" }).click();
  await page.locator(".row.group").first().waitFor();
  await page.locator(".row.group").first().click();
  await page.locator(".row[data-id]").first().waitFor();
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Aucun" }).click();
  await page.waitForFunction(() => !document.querySelector(".row.group"));
});

await step("saved view: save as, apply, duplicate name rejected by constraint", async () => {
  const name = `Par projet ${Date.now() % 100000}`;
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Projet" }).click();
  await page.locator(".row.group").first().waitFor();
  const save = async () => {
    await page.getByRole("button", { name: /^Vues|^Par projet/ }).click();
    await page.getByRole("menuitem", { name: "Enregistrer la vue sous…" }).click();
    await page.locator("#sv-name").fill(name);
    await page.locator(".dialog-footer").getByRole("button", { name: "Enregistrer" }).click();
  };
  await save();
  await page.getByRole("button", { name }).waitFor();
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Aucun" }).click();
  await page.waitForFunction(() => !document.querySelector(".row.group"));
  await page.getByRole("button", { name }).click();
  await page.getByRole("menuitem", { name }).click();
  await page.locator(".row.group").first().waitFor();
  await save();
  await toast("Une vue porte déjà ce nom");
  await page.locator(".dialog-footer").getByRole("button", { name: "Annuler" }).click();
  await page.locator("#group-activities").click();
  await page.getByRole("option", { name: "Aucun" }).click();
  await page.waitForFunction(() => !document.querySelector(".row.group"));
});

await step("screenshots: activities light", async () => {
  await home();
  await page.screenshot({ path: `${shots}activities-light.png` });
});

await step("edit a row in the drawer (partial update, live refresh, history)", async () => {
  const desc = page.locator("#f-description");
  await page.locator(".row[data-id]").nth(0).click();
  await page.keyboard.press("Enter");
  for (let i = 0; await desc.getAttribute("readonly").catch(() => "") !== null; i++) {
    check(i < 30, "an editable row");
    await page.keyboard.press("ArrowDown");
    await page.waitForTimeout(150);
  }
  const text = `Revue ${Date.now() % 100000}`;
  await desc.fill(text);
  await page.getByRole("button", { name: "Enregistrer" }).click();
  await toast("Enregistré");
  const d = await colIndex("Description");
  await page.waitForFunction(([i, text]) => document.querySelectorAll(".row.selected .cell")[i + 1]?.textContent?.trim() === text, [d, text] as const);
  await page.getByRole("tab", { name: "Historique" }).click();
  await page.locator(".history li", { hasText: text }).waitFor();
  await page.getByRole("tab", { name: "Détails" }).click();
});

await step("screenshots: drawer light", async () => {
  await home();
  await page.screenshot({ path: `${shots}drawer-light.png` });
});

await step("rejected command: move a row into a locked period (Ensure)", async () => {
  const locked = new Date(Date.now() - 300 * 864e5);
  await page.locator("#f-date").locator('[data-segment="day"]').click();
  await page.keyboard.type(`${String(locked.getDate()).padStart(2, "0")}${String(locked.getMonth() + 1).padStart(2, "0")}${locked.getFullYear()}`);
  await page.getByRole("button", { name: "Enregistrer" }).click();
  await toast("refusé");
  check(await page.locator(".toast li", { hasText: "Période non verrouillée" }).count(), "condition label in toast");
});

await step("command disabled with condition tooltip", async () => {
  await page.locator(".drawer-head button").click();
  const btn = page.locator('[data-command="activity.unvalidate"]');
  check(await btn.isDisabled(), "unvalidate disabled on unvalidated rows");
  await btn.locator("..").hover();
  await page.locator(".tooltip", { hasText: "Validée" }).waitFor();
});

await step("switch theme to dark", async () => {
  await page.getByTestId("user-menu").click();
  await page.getByRole("menuitem", { name: "Sombre" }).click();
  check((await page.locator("html").getAttribute("data-theme")) === "dark", "data-theme=dark");
  await page.locator(".row.selected").first().dblclick();
  await page.locator(".drawer form").waitFor();
  await home();
  await page.mouse.move(0, 0);
  await page.screenshot({ path: `${shots}drawer-dark.png` });
  await page.locator(".drawer-head button").click();
  await home();
  await page.screenshot({ path: `${shots}activities-dark.png` });
});

await step("open invoicing (generic screen 2), run with live progress", async () => {
  await page.goto(`${base}/#/invoicing`);
  await ready(page, "invoicing");
  await page.locator('[data-command="invoice.run"]').click();
  await page.getByRole("button", { name: "Démarrer" }).click();
  await page.locator('[data-run-state="done"]').waitFor({ timeout: 20000 });
  await page.locator(".dialog-footer").getByRole("button", { name: "Fermer" }).click();
});

await step("open profitability (generic screen 3, grouped with aggregates)", async () => {
  await page.goto(`${base}/#/profitability`);
  await ready(page, "profitability");
  check(await page.locator(".row.group").count(), "groups shown");
});

await step("upload a receipt through a child screen command (sha256, presign, PUT, register)", async () => {
  const post = (v: string, body: object) => fetch(`${base}/api/v/${v}`, { method: "POST", body: JSON.stringify(body) }).then((r) => r.json());
  const expenses: { activity_id: number }[] = await post("expense", { limit: 5000 });
  const open: { id: number }[] = await post("activity_grid", {
    params: { date_from: "2000-01-01", date_to: "2100-01-01" },
    filter: [{ col: "id", op: "in", value: expenses.map((e) => e.activity_id) }, { col: "not_validated", op: "eq", value: true },
      { col: "not_locked", op: "eq", value: true }, { col: "not_invoiced", op: "eq", value: true }],
    limit: 1,
  });
  const state = { wrap: { filter: [{ col: "id", op: "eq", value: open[0].id }] } };
  await page.goto(`${base}/#/activities?s=${encodeURIComponent(JSON.stringify(state))}`);
  await ready(page, "activities");
  await page.locator(".row[data-id]").first().dblclick();
  await page.getByRole("tab", { name: "Frais" }).click();
  await page.locator('[data-screen="activity.expenses"][data-ready] .row[data-id]').first().click();
  await page.locator('[data-command="expense.attach_receipt"]').click();
  const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");
  await page.locator('.dialog input[type="file"]').setInputFiles({ name: "ticket.png", mimeType: "image/png", buffer: png });
  await page.locator(".dialog img").waitFor();
  await page.locator(".dialog-footer").getByRole("button", { name: "Exécuter" }).click();
  await page.locator('[data-screen="activity.expenses"] .row[data-id] svg').first().waitFor();
});

await step("switch language to English", async () => {
  await page.getByTestId("user-menu").click();
  await page.getByRole("menuitem", { name: "English" }).click();
  await page.locator("h1", { hasText: "Activities" }).waitFor();
  await page.getByTestId("user-menu").click();
  await page.getByRole("menuitem", { name: "Français" }).click();
  await page.getByTestId("user-menu").click();
  await page.getByRole("menuitem", { name: "Clair" }).click();
});

await browser.close();
if (errors.length) {
  console.log("page errors:\n" + errors.join("\n"));
  process.exit(1);
}
console.log("smoke passed");

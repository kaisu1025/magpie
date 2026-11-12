// Run with Node's test runner and Playwright on the module path; see README.md.
// Settings › Models offers the image recognition model's thinking levels.
// Verify selection, persistence, and fit in Chromium and WebKit.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");
const { watchSettingsReads, settingsDrawn, reach } = require("./reader.cjs");

const assets = path.resolve(__dirname, "../assets");
// the menu's head and Default, said in each language
const words = {
  en: { head: "Thinking", dflt: "Default", visionNote: "the least it has" },
  zh: { head: "思考", dflt: "默认", visionNote: "它最低的一档" },
  "zh-TW": { head: "思考", dflt: "預設", visionNote: "它最低的一檔" },
  ja: { head: "思考", dflt: "デフォルト", visionNote: "最も低いレベル" },
  de: { head: "Denken", dflt: "Standard", visionNote: "seine niedrigste Stufe" },
};

function serve(lang, posted, st) {
  const settings = () => ({ lang, theme: "light", gateway: "http://127.0.0.1:3999", port: 3999,
    vision: st.vision, visionAuto: "codex/gpt-6.1-sol",
    visionModels: [{ id: "codex/gpt-6.1-sol", name: "GPT-6.1 Sol", provider: "codex", providerName: "Codex", icon: "openai" }],
    visionEfforts: st.vision === "off" ? undefined : st.visionLevels, visionEffort: st.visionEffort, visionEffortNow: st.visionEffort || undefined });
  return async (r) => {
    const url = new URL(r.request().url());
    const json = (data) => r.fulfill({ json: data });
    if (url.pathname === "/boot.js") return r.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = ${JSON.stringify({ lang, theme: "light", web: true })};` });
    if (url.pathname === "/wails/runtime.js") return r.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json({ agents: [], profiles: [], settings: settings() });
    if (url.pathname === "/api/settings") {
      if (r.request().method() === "POST") {
        const b = JSON.parse(r.request().postData());
        posted.push(b);
        Object.assign(st, { vision: b.vision, visionEffort: b.visionEffort });
      }
      return json(settings());
    }
    if (url.pathname === "/api/plugins") return json({ plugins: [] });
    if (url.pathname === "/api/groups") return json({ models: [], groups: [], pools: [] });
    if (url.pathname === "/api/providers") return json({ providers: [], presets: [], excluded: [], gateway: { running: true, window: true, url: "http://127.0.0.1:3999" } });
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    const body = await fs.readFile(file).catch(() => null);
    await (body ? r.fulfill({ body, contentType }) : r.fulfill({ status: 404, body: "" }));
  };
}

const chipSelector = "#imagesList button.vision-effort";

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    const w = words[lang];
    test(`${engine} ${lang}: Image recognition can set how hard its model thinks`, async (t) => {
      const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
      t.after(() => browser.close());
      const page = await (await browser.newContext({ reducedMotion: "reduce" })).newPage();
      page.setDefaultTimeout(5000);
      page.setDefaultNavigationTimeout(60000);
      const errors = [];
      page.on("pageerror", (e) => errors.push(e.message));
      const posted = [];
      const st = { vision: "codex/gpt-6.1-sol", visionEffort: "", visionLevels: ["low", "medium", "high", "xhigh"] };
      await page.route("**/*", serve(lang, posted, st));
      await watchSettingsReads(page);
      const where = () => page.evaluate(() => [scrollX, scrollY, document.scrollingElement.scrollTop, document.querySelector(".view:not([hidden]) ")?.scrollTop ?? 0, document.querySelector("main")?.scrollTop ?? 0]);
      const press = async (loc) => {
        const b = await loc.boundingBox();
        await page.mouse.click(b.x + b.width / 2, b.y + b.height / 2);
      };
      for (const width of [1100, 420]) {
        await page.mouse.move(0, 0);
        await page.setViewportSize({ width, height: 900 });
        await page.goto("http://magpie.test/?view=settings&tab=models");
        await settingsDrawn(page);
        const tr = (k) => page.evaluate((k) => t(k), k);
        const chip = page.locator(chipSelector);
        await reach(page, chip);
        // the chip fits its row, beside the model's, and the page doesn't scroll sideways
        const fit = await chip.evaluate((c) => {
          const r = c.closest(".row").getBoundingClientRect(), b = c.getBoundingClientRect(), m = c.previousElementSibling.getBoundingClientRect();
          return { inside: b.left >= r.left - 1 && b.right <= r.right + 1, beside: m.right <= b.left + 1 || m.bottom <= b.top + 1,
            wide: document.documentElement.scrollWidth - document.documentElement.clientWidth, tag: c.tagName, native: !!c.closest(".row").querySelector("select") };
        });
        assert(fit.inside && fit.beside && fit.wide <= 0 && !fit.native, `Image recognition ${width}: ${JSON.stringify(fit)}`);
        assert.equal(await chip.innerText(), w.dflt, `Image recognition: nothing picked reads Default`);
        if (process.env.ARTIFACT_DIR) {
          await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
          await chip.locator("xpath=ancestor::div[contains(@class,'row')][1]").screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-visionEffort.png`) });
        }
        const before = await where();
        // the menu: Default, then only the model's own levels
        await press(chip);
        const menu = page.locator(".proto-menu.side-effort-menu");
        await menu.waitFor({ state: "visible" });
        assert.equal(await menu.locator(".pm-head").innerText(), w.head);
        const names = await menu.locator(".pm-name").allInnerTexts();
        const want = [w.dflt, ...await Promise.all(st.visionLevels.map(tr))];
        assert.deepEqual(names, want, `Image recognition: only the model's levels`);
        if (lang !== "en") for (const l of st.visionLevels) assert.notEqual(await tr(l), l, `${l} is said in ${lang}`);
        assert.equal(await menu.locator(".pm-item").first().locator(".pm-note").innerText(), w.visionNote);
        assert.equal(await menu.locator(".pm-item.on .pm-name").innerText(), w.dflt);
        const mb = await menu.boundingBox();
        assert(mb.x >= 0 && mb.x + mb.width <= width + 1, `menu in the window at ${width}: ${JSON.stringify(mb)}`);
        // a level picked is saved and shown
        await press(menu.locator(".pm-item").nth(3));
        await page.waitForFunction(([sel, k]) => document.querySelector(sel)?.innerText === k, [chipSelector, await tr(st.visionLevels[2])]);
        assert.equal(posted.at(-1).visionEffort, st.visionLevels[2]);
        await menu.waitFor({ state: "detached" });
        // ticked when opened again, and Default clears it
        await press(page.locator(chipSelector));
        await menu.waitFor({ state: "visible" });
        assert.equal(await menu.locator(".pm-item.on .pm-name").innerText(), await tr(st.visionLevels[2]));
        await press(menu.locator(".pm-item").first());
        await page.waitForFunction(([sel, d]) => document.querySelector(sel)?.innerText === d, [chipSelector, w.dflt]);
        assert.equal(posted.at(-1).visionEffort, "");
        // the chip again closes its menu, Escape too, and neither saves
        const n = posted.length;
        await press(page.locator(chipSelector));
        await menu.waitFor({ state: "visible" });
        await press(page.locator(chipSelector));
        await menu.waitFor({ state: "detached" });
        await press(page.locator(chipSelector));
        await menu.waitFor({ state: "visible" });
        await page.keyboard.press("Escape");
        await menu.waitFor({ state: "detached" });
        assert.equal(posted.length, n);
        assert.deepEqual(await where(), before, `Image recognition ${width}: the clicks moved nothing`);
        // Saving a separate preference keeps the selected effort.
        st.visionEffort = "high";
        await page.evaluate(() => fetch("/api/settings").then((r) => r.json()).then((s) => { prefs = s; state.settings = s; renderSettings(); }));
        await page.waitForFunction((sel) => document.querySelector(sel)?.innerText === t("high"), chipSelector);
        await page.evaluate(() => savePrefs({ ...prefsKeep(prefs), noStats: true }));
        assert.equal(posted.at(-1).visionEffort, "high");
        st.visionEffort = "";
      }
      // No chip when the model has no known levels.
      st.visionLevels = undefined;
      await page.goto("http://magpie.test/?view=settings&tab=models");
      await settingsDrawn(page);
      assert.equal(await page.locator(chipSelector).count(), 0);
      assert.deepEqual(errors, []);
    });
  }
}

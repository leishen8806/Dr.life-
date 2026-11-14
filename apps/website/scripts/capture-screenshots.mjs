import { createReadStream, existsSync, statSync } from "node:fs";
import { mkdir, rm, writeFile } from "node:fs/promises";
import { createServer } from "node:http";
import { extname, join, normalize, posix, relative } from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";

const root = fileURLToPath(new URL("../dist/", import.meta.url));
const output = fileURLToPath(new URL("../screenshots/", import.meta.url));
const widths = [360, 390, 768, 1280, 1440, 1920];
const languages = ["zh", "en", "km"];
const pages = [{ name: "home", path: "/" }, { name: "merchant", path: "/merchant/" }];
const sectionShots = [
  ["header", ".topbar"],
  ["hero", ".hero"],
  ["business", "#business"],
  ["route", ".route"],
  ["apps", ".access-section"],
  ["partners", ".partners"],
  ["footer", ".footer"]
];
const contentTypes = {
  ".css": "text/css", ".html": "text/html; charset=utf-8", ".ico": "image/x-icon",
  ".js": "text/javascript", ".json": "application/json", ".svg": "image/svg+xml",
  ".woff2": "font/woff2", ".webp": "image/webp", ".avif": "image/avif",
  ".png": "image/png", ".jpg": "image/jpeg"
};

function safeFileForUrl(requestUrl) {
  const pathname = new URL(requestUrl, "http://127.0.0.1").pathname;
  const requestedPath = pathname === "/" ? "index.html" : pathname.replace(/^\/+/, "");
  const candidate = normalize(join(root, requestedPath));
  const relativePath = relative(root, candidate);
  if (relativePath.startsWith("..") || posix.isAbsolute(relativePath)) return null;
  if (existsSync(candidate) && statSync(candidate).isFile()) return candidate;
  if (existsSync(join(candidate, "index.html"))) return join(candidate, "index.html");
  return null;
}

async function serve() {
  const server = createServer((request, response) => {
    const file = safeFileForUrl(request.url ?? "/");
    if (!file) { response.writeHead(404); response.end("Not found"); return; }
    response.setHeader("Content-Type", contentTypes[extname(file)] ?? "application/octet-stream");
    createReadStream(file).pipe(response);
  });
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = server.address();
  if (!address || typeof address === "string") throw new Error("Unable to start screenshot server");
  return { server, origin: `http://127.0.0.1:${address.port}` };
}

await rm(output, { recursive: true, force: true });
await mkdir(output, { recursive: true });
const { server, origin } = await serve();
const browser = await chromium.launch();
const manifest = [];

try {
  for (const width of widths) {
    for (const language of languages) {
      for (const pageDefinition of pages) {
        const context = await browser.newContext({
          viewport: { width, height: 900 },
          locale: language === "zh" ? "zh-CN" : language === "km" ? "km-KH" : "en-US",
          deviceScaleFactor: 1
        });
        const page = await context.newPage();
        const failedResources = [];
        page.on("response", (resourceResponse) => {
          if (resourceResponse.request().resourceType() !== "document" && resourceResponse.status() >= 400) {
            failedResources.push(`${resourceResponse.status()} ${resourceResponse.url()}`);
          }
        });
        const url = new URL(pageDefinition.path, origin);
        url.searchParams.set("lang", language);
        const response = await page.goto(url.toString(), { waitUntil: "networkidle" });
        if (!response?.ok()) throw new Error(`Screenshot page failed: ${url} (${response?.status() ?? "no response"})`);
        if (failedResources.length > 0) {
          throw new Error(`Screenshot page loaded failed static resources: ${failedResources.join(", ")}`);
        }
        const layout = await page.evaluate(() => {
          const heading = document.querySelector("h1");
          const body = document.body;
          const rect = heading?.getBoundingClientRect();
          return {
            headingVisible: Boolean(rect && rect.top >= 0 && rect.bottom <= window.innerHeight),
            overflow: body.scrollWidth > window.innerWidth + 1,
            chineseRuns: [...document.body.querySelectorAll("body *")]
              .filter((element) => !["SCRIPT", "STYLE", "NOSCRIPT"].includes(element.tagName))
              .flatMap((element) => [...element.childNodes]
                .filter((node) => node.nodeType === Node.TEXT_NODE && node.textContent?.trim())
                .map((node) => node.textContent.trim()))
              .filter((text) => /[\u4e00-\u9fff]{2,}/.test(text))
          };
        });
        if (!layout.headingVisible) throw new Error(`Homepage h1 is not visible at ${width}px (${language}, ${pageDefinition.name})`);
        if (layout.overflow) throw new Error(`Horizontal overflow at ${width}px (${language}, ${pageDefinition.name})`);
        if (language !== "zh" && pageDefinition.name === "home" && layout.chineseRuns.length > 0) {
          throw new Error(`Visible Chinese copy remains at ${width}px (${language}): ${layout.chineseRuns.slice(0, 5).join(" | ")}`);
        }
        if (width === 1920 && language === "zh" && pageDefinition.name === "home") {
          const routeTitle = await page.locator("#route-title").textContent();
          if (!routeTitle?.includes("从中国工厂") || !routeTitle.includes("到柬埔寨消费者手中")) {
            throw new Error(`Route title is not semantically split at ${width}px: ${routeTitle ?? "missing"}`);
          }
        }
        if (width === 1920 && pageDefinition.name === "home") {
          const geometry = await page.evaluate(() => {
            const navItems = [...document.querySelectorAll(".topbar .nav > a:not(.nav-cta)")]
              .filter((item) => getComputedStyle(item).display !== "none" && item.getBoundingClientRect().width > 0);
            const partnerContainer = document.querySelector(".section.partners > .container");
            const partnerGrid = document.querySelector(".partner-grid");
            const partnerCards = [...document.querySelectorAll(".partner-grid .partner")];
            const accessGrid = document.querySelector(".app-grid");
            const accessCards = [...document.querySelectorAll(".app-grid .app-card")];
            const businessContainer = document.querySelector("#business > .container");
            const routeContainer = document.querySelector(".route .container");
            const steps = document.querySelector(".route .steps");
            const channelList = document.querySelector(".channel-list");
            const skip = document.querySelector(".skip");
            const rounded = (value) => Math.round(value * 10) / 10;
            return {
              navTops: navItems.map((item) => Math.round(item.getBoundingClientRect().top)),
              partnerMinWidth: Math.min(...partnerCards.map((item) => item.getBoundingClientRect().width)),
              partnerLeftDelta: rounded((partnerContainer?.getBoundingClientRect().left ?? 0) - (businessContainer?.getBoundingClientRect().left ?? 0)),
              partnerColumns: getComputedStyle(partnerGrid).gridTemplateColumns.split(" ").length,
              partnerTopRows: new Set(partnerCards.map((item) => Math.round(item.getBoundingClientRect().top))).size,
              partnerBackground: getComputedStyle(partnerGrid).backgroundColor,
              accessColumns: getComputedStyle(accessGrid).gridTemplateColumns.split(" ").length,
              accessTopRows: new Set(accessCards.map((item) => Math.round(item.getBoundingClientRect().top))).size,
              accessLeftDelta: rounded((accessGrid?.getBoundingClientRect().left ?? 0) - (businessContainer?.getBoundingClientRect().left ?? 0)),
              channelMarginTop: Number.parseFloat(getComputedStyle(channelList).marginTop),
              stepsRatio: (steps?.getBoundingClientRect().width ?? 0) / (routeContainer?.getBoundingClientRect().width ?? 1),
              stepColumns: getComputedStyle(steps).gridTemplateColumns.split(" ").length,
              skipVisible: skip ? getComputedStyle(skip).clipPath !== "inset(50%)" : true
            };
          });
          if (new Set(geometry.navTops).size !== 1) throw new Error("Header navigation wrapped at 1920px");
          if (geometry.partnerMinWidth < 260) throw new Error(`Partner cards are too narrow: ${geometry.partnerMinWidth}px`);
          if (Math.abs(geometry.partnerLeftDelta) > 2) throw new Error(`Partner section is not aligned: ${geometry.partnerLeftDelta}px`);
          const partnerGridIsPacked = (geometry.partnerColumns === 5 && geometry.partnerTopRows === 1)
            || (geometry.partnerColumns === 3 && geometry.partnerTopRows === 2);
          if (!partnerGridIsPacked || geometry.partnerBackground !== "rgba(0, 0, 0, 0)") throw new Error(`Partner grid has an empty slot: ${geometry.partnerColumns} columns / ${geometry.partnerTopRows} rows / ${geometry.partnerBackground}`);
          if (Math.abs(geometry.accessLeftDelta) > 2) throw new Error(`Access grid is not aligned: ${geometry.accessLeftDelta}px`);
          if (geometry.accessColumns !== 3 || geometry.accessTopRows !== 1) throw new Error(`Access grid has an empty slot: ${geometry.accessColumns} columns / ${geometry.accessTopRows} rows`);
          if (geometry.channelMarginTop < 24) throw new Error(`Official channels lack vertical separation: ${geometry.channelMarginTop}px`);
          if (geometry.stepsRatio < 0.98 || geometry.stepColumns !== 4) throw new Error(`Route steps use too little width: ${geometry.stepsRatio} / ${geometry.stepColumns} columns`);
          if (geometry.skipVisible) throw new Error("Skip link is visible before focus");
        }
        if (pageDefinition.name === "home") {
          const homepageGeometry = await page.evaluate(() => {
            const overlap = (a, b) => a && b && a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
            const sections = [".market", ".route", ".access-section", ".partners"].map((selector) => {
              const node = document.querySelector(selector);
              const rect = node?.getBoundingClientRect();
              const background = node ? getComputedStyle(node).backgroundColor : "";
              return { selector, left: rect?.left ?? 0, right: rect?.right ?? 0, background };
            });
            const partnerGrid = document.querySelector(".partner-grid");
            const apps = document.querySelector("#apps");
            const navItems = [...document.querySelectorAll(".topbar .nav a")]
              .filter((item) => getComputedStyle(item).display !== "none" && item.getBoundingClientRect().width > 0);
            const language = document.querySelector(".header-actions .languages");
            const shop = document.querySelector(".header-actions .shop-link");
            const partnerContainer = document.querySelector(".section.partners > .container");
            const accessContainer = document.querySelector(".access-section > .container");
            const businessContainer = document.querySelector("#business > .container");
            const channelItems = [...document.querySelectorAll(".channel-list span")];
            const heroButtons = [...document.querySelectorAll(".hero .actions .button")];
            const values = document.querySelector(".values");
            const accessSection = document.querySelector(".access-section");
            const accessGrid = document.querySelector(".access-grid");
            return {
              sections,
              partnerLeftDelta: (partnerContainer?.getBoundingClientRect().left ?? 0) - (businessContainer?.getBoundingClientRect().left ?? 0),
              accessLeftDelta: (partnerContainer?.getBoundingClientRect().left ?? 0) - (accessContainer?.getBoundingClientRect().left ?? 0),
              partnerGap: (apps?.getBoundingClientRect().top ?? 0) - (partnerGrid?.getBoundingClientRect().bottom ?? 0),
              navOverlap: navItems.some((item) => {
                const rect = item.getBoundingClientRect();
                return overlap(rect, language?.getBoundingClientRect()) || overlap(rect, shop?.getBoundingClientRect());
              }),
              navFontSizes: navItems.map((item) => Number.parseFloat(getComputedStyle(item).fontSize)),
              channelItems,
              heroButtonTops: heroButtons.map((item) => Math.round(item.getBoundingClientRect().top)),
              heroButtonRights: heroButtons.map((item) => Math.round(item.getBoundingClientRect().right)),
              valuesBackground: values ? getComputedStyle(values).backgroundColor : "",
              accessBackground: accessSection ? getComputedStyle(accessSection).backgroundColor : "",
              accessGridBackground: accessGrid ? getComputedStyle(accessGrid).backgroundColor : ""
            };
          });
          for (const section of homepageGeometry.sections) {
            if (section.left > 1 || section.right < width - 1) throw new Error(`Homepage section is not full-bleed at ${width}px (${language}): ${section.selector} ${section.left}..${section.right}`);
          }
          if ([360, 390, 768].includes(width) && Math.abs(homepageGeometry.partnerLeftDelta) > 2) {
            throw new Error(`Partner left edge is not aligned at ${width}px (${language}): ${homepageGeometry.partnerLeftDelta}px`);
          }
          if ([360, 390, 768].includes(width) && homepageGeometry.heroButtonRights.some((right) => right > width)) {
            throw new Error(`Hero button exceeds viewport at ${width}px (${language}): ${homepageGeometry.heroButtonRights.join(",")}`);
          }
          if ([1280, 1440, 1920].includes(width) && homepageGeometry.navOverlap) {
            throw new Error(`Header navigation overlaps actions at ${width}px (${language})`);
          }
          if ([1280, 1440].includes(width)) {
            const navLink = page.locator(".nav a").first();
            const appLink = page.locator(".nav-apps");
            if (!(await navLink.isVisible()) || !(await appLink.isVisible())) {
              throw new Error(`Desktop navigation or App entry is hidden at ${width}px (${language})`);
            }
          }
          if (width >= 1441) {
            const desktopNav = page.locator(".nav a").first();
            const appLink = page.locator(".nav-apps");
            if (!(await desktopNav.isVisible()) || !(await appLink.isVisible())) {
              throw new Error(`Desktop navigation or App entry is hidden at ${width}px (${language})`);
            }
          }
          if (homepageGeometry.navFontSizes.length > 0) {
            const minimumNavFontSize = language === "km" ? 13 : 12;
            if (homepageGeometry.navFontSizes.some((size) => size < minimumNavFontSize)) {
              throw new Error(`Header navigation font is below ${minimumNavFontSize}px at ${width}px (${language}): ${homepageGeometry.navFontSizes.join(",")}`);
            }
          }
          if ([1440, 1920].includes(width) && Math.abs(homepageGeometry.accessLeftDelta) > 2) {
            throw new Error(`Partners and app sections are not aligned at ${width}px (${language}): ${homepageGeometry.accessLeftDelta}px`);
          }
          if (homepageGeometry.channelItems.some((item, index, items) => index > 0 && item.top === items[index - 1].top && item.left - items[index - 1].right < 12)) {
            throw new Error(`Official channel items lack visible separation at ${width}px (${language})`);
          }
          if (width === 390 && language === "zh") {
            if (new Set(homepageGeometry.heroButtonTops).size !== 1) {
              throw new Error(`Hero actions are not on one row at ${width}px (${language}): ${homepageGeometry.heroButtonTops.join(",")}`);
            }
            if (homepageGeometry.valuesBackground !== "rgba(0, 0, 0, 0)") {
              throw new Error(`Culture values container has a filled background at ${width}px (${language}): ${homepageGeometry.valuesBackground}`);
            }
            if (homepageGeometry.accessGridBackground !== "rgba(0, 0, 0, 0)") {
              throw new Error(`App cards wrapper has a filled background at ${width}px (${language}): ${homepageGeometry.accessGridBackground}`);
            }
            if (!["rgb(32, 33, 31)", "rgb(23, 24, 23)"].includes(homepageGeometry.accessBackground)) {
              throw new Error(`App section is not dark at ${width}px (${language}): ${homepageGeometry.accessBackground}`);
            }
          }
          if ([1440, 1920].includes(width) && homepageGeometry.partnerGap < 64) {
            throw new Error(`Partner cards touch the app section at ${width}px (${language}): ${homepageGeometry.partnerGap}px gap`);
          }
        }
        await page.waitForTimeout(150);
        const relative = posix.join(String(width), language, `${pageDefinition.name}.png`);
        await mkdir(join(output, String(width), language), { recursive: true });
        await page.screenshot({ path: join(output, relative), fullPage: true });
        manifest.push(`${relative}\t${url.pathname}?lang=${language}`);
        if (pageDefinition.name === "home" && [390, 1440].includes(width)) {
          for (const [section, selector] of sectionShots) {
            const sectionNode = page.locator(selector);
            const sectionRelative = posix.join("sections", String(width), language, `${section}.png`);
            await mkdir(join(output, "sections", String(width), language), { recursive: true });
            await sectionNode.screenshot({ path: join(output, sectionRelative) });
            manifest.push(`${sectionRelative}\t${url.pathname}?lang=${language}#${section}`);
          }
        }
        await context.close();
      }
    }
  }
} finally {
  await browser.close();
  await new Promise((resolve) => server.close(resolve));
}

await writeFile(join(output, "manifest.txt"), `${manifest.join("\n")}\n`, "utf8");
const expected = widths.length * languages.length * pages.length + 2 * languages.length * sectionShots.length;
if (manifest.length !== expected) throw new Error(`Expected ${expected} screenshots, captured ${manifest.length}`);
console.log(`Captured ${manifest.length} screenshots in ${output}`);

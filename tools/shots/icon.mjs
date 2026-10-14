// The app icon, rasterised from the one the page wears. macOS wants pixels
// and internal/ui/icon.svg is the only drawing of the mark, so this is how
// the two stay the same icon.
//
// Usage: node icon.mjs <in.svg> <out.png> [size]
import { readFileSync, writeFileSync } from "node:fs";
import puppeteer from "puppeteer-core";

const [svgPath, outPath, size = "1024"] = process.argv.slice(2);
if (!svgPath || !outPath) {
  console.error("usage: node icon.mjs <in.svg> <out.png> [size]");
  process.exit(2);
}

const svg = readFileSync(svgPath, "utf8");
const side = Number(size);

const browser = await puppeteer.launch({
  executablePath: process.env.ADE_CHROME || "/usr/bin/chromium",
  headless: true,
  args: ["--no-sandbox", "--disable-dev-shm-usage", "--disable-gpu"],
});
const page = await browser.newPage();
await page.setViewport({ width: side, height: side, deviceScaleFactor: 1 });
// Transparent behind it: the mark draws its own rounded square, and a white
// page behind that is a white square in the dock.
await page.setContent(
  `<style>html,body{margin:0;background:transparent}svg{display:block;width:${side}px;height:${side}px}</style>${svg}`,
);
const shot = await page.screenshot({ omitBackground: true, type: "png" });
writeFileSync(outPath, shot);
await browser.close();
console.log(`${outPath} ${side}x${side}`);

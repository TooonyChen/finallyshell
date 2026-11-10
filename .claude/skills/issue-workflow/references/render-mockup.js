// Renders HTML mockups to PNG, at twice their size, as people see them.
//
//   node render-mockup.js page.html [more.html ...]
//
// Each page.html becomes page.png beside it, a screenshot of its <body>:
// size the body (a width in CSS) to frame the picture. Uses the
// Playwright and Chromium the sandbox has installed; don't install
// another.
const path = require('path');
const { chromium } = require('playwright');

(async () => {
  const pages = process.argv.slice(2);
  if (pages.length === 0) {
    console.error('usage: node render-mockup.js page.html [more.html ...]');
    process.exit(2);
  }
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1300, height: 800 }, deviceScaleFactor: 2 });
  for (const file of pages) {
    const abs = path.resolve(file);
    await page.goto('file://' + abs);
    await page.evaluate(() => document.fonts.ready);
    const out = abs.replace(/\.html?$/, '') + '.png';
    await page.locator('body').screenshot({ path: out });
    console.log(out);
  }
  await browser.close();
})();

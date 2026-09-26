/* Run against an isolated Curio instance. Requires Playwright and its WebKit browser. */
const { chromium, webkit } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const baseURL = process.env.CURIO_TEST_URL || 'http://127.0.0.1:18080';
const password = process.env.CURIO_TEST_PASSWORD || 'curio-local-qa-password';
const output = process.env.CURIO_TEST_OUTPUT || 'test-results';
fs.mkdirSync(output, { recursive: true });

async function verify(browserType, name, options = {}) {
  const browser = await browserType.launch(options);
  const context = await browser.newContext({ viewport: { width: 1440, height: 1040 }, colorScheme: 'light', reducedMotion: 'reduce' });
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  page.on('response', response => { if (response.status() >= 500) errors.push(`${response.status()} ${response.url()}`); });
  try {
    await page.goto(baseURL);
    await page.getByRole('heading', { name: 'Where will your curiosity take you?' }).waitFor();
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
    await page.screenshot({ path: `${output}/${name}-home-desktop.png`, fullPage: true });
    await page.keyboard.press(name === 'webkit' ? 'Alt+Tab' : 'Tab');
    assert.equal(await page.evaluate(() => document.activeElement.textContent), 'Skip to content');
    for (const kind of ['place', 'fact', 'surprise']) {
      await page.goto(`${baseURL}/discover?kind=${kind}`);
      const discoveryID = new URL(page.url()).searchParams.get('id');
      await page.getByRole('button', {name:'Just show me'}).click();
      await page.locator('.read-more > summary').click();
      assert.ok(await page.locator('.story-sources li a').first().getAttribute('href'));
      const photo = page.locator('.story-photo img');
      await photo.evaluate(img => img.decode());
      assert.ok(await photo.evaluate(img => img.naturalWidth >= 400));
      assert.ok(await page.locator('.story-body section').count() >= 2);
      assert.ok(await page.locator('.figures-panel dd').count() >= 2);
      await page.locator('.discovery-bottom .button').click();
      assert.notEqual(new URL(page.url()).searchParams.get('id'), discoveryID, 'immediate card repeated');
    }
    await page.screenshot({ path: `${output}/${name}-discovery-desktop.png`, fullPage: true });
    await page.goto(`${baseURL}/enter`);
    await page.getByLabel('Your username').fill(`qa-${name}`);
    await page.getByLabel('Household password', { exact: true }).fill(password);
    await page.getByRole('button', { name: 'Show password' }).click();
    assert.equal(await page.locator('#password').getAttribute('type'), 'text');
    await page.getByRole('button', { name: 'Hide password' }).click();
    await page.getByRole('button', { name: 'Let me in' }).click();
    await page.waitForURL(`${baseURL}/`);
    // Play three distinct stories; wrong answers still earn participation points.
    for (const id of ['octopus','venus','neutron']) {
      await page.goto(`${baseURL}/discover?kind=fact&id=${id}`);
      await page.locator('.guess-option').first().click();
      await page.locator('.answer-reveal').waitFor();
      assert.match(await page.locator('.points-pill').innerText(), /3 points banked/);
    }
    await page.reload();
    await page.getByRole('button',{name:'✦ Interesting',exact:true}).click();
    assert.equal(await page.getByRole('button',{name:'✦ Interesting',exact:true}).getAttribute('aria-pressed'),'true');
    await page.getByRole('button',{name:'Not for me',exact:true}).click();
    assert.equal(await page.getByRole('button',{name:'Not for me',exact:true}).getAttribute('aria-pressed'),'true');
    await page.locator('.read-more > summary').click();
    await page.screenshot({path:`${output}/${name}-reveal-desktop.png`,fullPage:true});
    await page.goto(baseURL);
    assert.match(await page.locator('.daily-score').innerText(),/9 Curiosity Points/);
    assert.match(await page.locator('.daily-score').innerText(),/3 \/ 3/);
    assert.match(await page.locator('.badge-shelf').innerText(),/Game for a guess/);
    await page.goto(`${baseURL}/discover?kind=fact&category=Animals`);
    assert.equal(new URL(page.url()).searchParams.get('id'),'octopus');
    await page.goto(`${baseURL}/discover?kind=fact&id=octopus`);
    await page.getByRole('button',{name:'♡ Keep this one',exact:true}).click();
    await page.getByRole('status').waitFor();
    await page.goto(`${baseURL}/collection`);
    await page.getByRole('heading',{name:'Three hearts. Eight busy arms.',exact:true}).waitFor();
    await page.screenshot({path:`${output}/${name}-collection-desktop.png`,fullPage:true});
    await page.getByRole('button',{name:'Remove Three hearts. Eight busy arms.',exact:true}).click();
    await page.getByRole('heading',{name:'Good stories deserve a souvenir.'}).waitFor();
    await page.getByRole('link',{name:'Recent detours',exact:true}).click();
    await page.getByRole('button',{name:'Save Three hearts. Eight busy arms.',exact:true}).click();
    await page.getByRole('status').waitFor();
    await page.getByRole('link',{name:'Your mix',exact:true}).click();
    assert.ok(await page.locator('.mix-row').count()>=4);
    await page.screenshot({path:`${output}/${name}-mix-desktop.png`,fullPage:true});
    await page.goto(`${baseURL}/settings`);
    await page.getByLabel('Display name').fill(`Explorer ${name}`);
    await page.getByRole('radio', { name: 'Light', exact: false }).check();
    await page.getByLabel('Space', { exact: true }).check();
    await page.getByLabel('Home label').fill('QA home');
    await page.getByLabel('Latitude', { exact: true }).fill('52.52');
    await page.getByLabel('Longitude', { exact: true }).fill('13.405');
    await page.getByRole('button', { name: 'Save my changes' }).click();
    await page.getByRole('status').waitFor();
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'light');
    await page.getByRole('radio', { name: 'Dark', exact: false }).check();
    await page.getByRole('button', { name: 'Save my changes' }).click();
    await page.getByRole('status').waitFor();
    assert.equal(await page.locator('html').getAttribute('data-theme'), 'dark');
    await page.reload();
    assert.equal(await page.getByLabel('Home label').inputValue(), 'QA home');
    assert.ok(await page.getByLabel('Space', { exact: true }).isChecked());
    await page.screenshot({ path: `${output}/${name}-settings-dark.png`, fullPage: true });
    await page.goto(`${baseURL}/collection?tab=mix`);
    await page.getByRole('button',{name:'Reset my mix',exact:true}).click();
    await page.getByRole('status').waitFor();
    assert.ok((await page.locator('.mix-row meter').evaluateAll(rows=>rows.map(row=>row.value))).every(value=>value===100));
    await page.goto(`${baseURL}/settings`);
    assert.equal(await page.getByLabel('Space',{exact:true}).isChecked(),false);
    assert.equal(await page.getByLabel('Home label').inputValue(),'QA home');
    await page.goto(`${baseURL}/collection`);
    await page.getByRole('heading',{name:'Three hearts. Eight busy arms.',exact:true}).waitFor();
    await page.setViewportSize({ width: 390, height: 844 });
    for (const path of ['/', '/settings', '/collection', '/collection?tab=history', '/collection?tab=mix', '/discover?kind=place&id=socotra']) {
      await page.goto(baseURL + path);
      assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), `mobile overflow: ${path}`);
    }
    await page.screenshot({ path: `${output}/${name}-discovery-mobile.png`, fullPage: true });
    await page.getByRole('button',{name:'Just show me'}).click();
    assert.match(await page.locator('.points-pill').innerText(),/1 point banked/);
    await page.screenshot({path:`${output}/${name}-reveal-mobile.png`,fullPage:true});
    await page.setViewportSize({width:320,height:640});
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'320px reveal overflow');
    await page.goto(`${baseURL}/discover?kind=place&id=bryce`);
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'320px quiz overflow');
    await page.goto(`${baseURL}/collection`);
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'320px collection overflow');
    await page.screenshot({path:`${output}/${name}-collection-mobile.png`,fullPage:true});
    await page.goto(`${baseURL}/collection?tab=mix`);
    assert.ok(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth),'320px mix overflow');
    await page.setViewportSize({width:390,height:844});
    await page.goto(`${baseURL}/`);
    await page.screenshot({ path: `${output}/${name}-home-mobile-dark.png`, fullPage: true });
    await page.goto(`${baseURL}/settings`);
    await page.getByRole('button', { name: 'Leave this profile' }).click();
    await page.waitForURL(`${baseURL}/enter`);
    await page.goto(`${baseURL}/settings`);
    await page.waitForURL(`${baseURL}/enter`);
    await page.screenshot({ path: `${output}/${name}-entry-mobile.png`, fullPage: true });
    await page.goto(`${baseURL}/`);
    await page.screenshot({ path: `${output}/${name}-home-mobile-default.png`, fullPage: true });
    await page.setViewportSize({ width: 320, height: 640 });
    assert.ok(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), '320px overflow');
    // A different browser profile must not inherit account data.
    const other = await browser.newContext();
    const guest = await other.newPage();
    await guest.goto(`${baseURL}/settings`);
    assert.equal(new URL(guest.url()).pathname, '/enter');
    await guest.goto(`${baseURL}/collection`);
    assert.equal(new URL(guest.url()).pathname,'/enter');
    await guest.goto(`${baseURL}/discover?kind=fact&id=octopus`);
    assert.equal(await guest.locator('.guess-option').count(),3);
    await other.close();
    // Private HTML must never enter the service worker cache.
    await page.evaluate(async () => { await navigator.serviceWorker.ready; });
    await page.reload();
    const cachedURLs = await page.evaluate(async () => {
      const keys = await caches.keys();
      return (await Promise.all(keys.map(async key => (await (await caches.open(key)).keys()).map(request => request.url)))).flat();
    });
    assert.ok(cachedURLs.every(url => new URL(url).pathname.startsWith('/static/')));
    // WebKit's emulated offline mode fails navigation before its worker handles fetch.
    // Exercise that path in Chromium; verify WebKit cache privacy above.
    if (name === 'chromium') {
      await context.setOffline(true);
      await page.goto(`${baseURL}/settings`);
      await page.getByRole('heading', { name: 'A little pause.' }).waitFor();
      assert.equal(await page.getByText('QA home').count(), 0);
      await context.setOffline(false);
    }
    assert.deepEqual(errors, []);
    console.log(`${name}: entry, settings, isolation, logout, discovery, responsive layouts, theme, and cache privacy passed`);
  } finally { await browser.close(); }
}
(async () => {
  if (process.env.CURIO_TEST_BROWSER !== 'webkit') await verify(chromium, 'chromium', process.env.CURIO_BROWSER_EXECUTABLE ? { executablePath: process.env.CURIO_BROWSER_EXECUTABLE } : {});
  if (process.env.CURIO_TEST_BROWSER !== 'chromium') await verify(webkit, 'webkit');
})().catch(error => { console.error(error); process.exit(1); });

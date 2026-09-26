/* Plain HTTP on a non-loopback host exercises the same origin fallback as phones on a LAN. */
const { chromium, webkit } = require('playwright');
const assert = require('node:assert/strict');
const os = require('node:os');
const fs = require('node:fs');
const localAddress = Object.values(os.networkInterfaces()).flat().find(address =>
  address && address.family === 'IPv4' && !address.internal && !address.address.startsWith('169.254.'));
const base = new URL(process.env.CURIO_TEST_LAN_URL || process.env.CURIO_TEST_URL || 'http://127.0.0.1:18080');
if (!process.env.CURIO_TEST_LAN_URL) {
  assert.ok(localAddress, 'A non-loopback IPv4 address is required for the LAN regression test');
  base.hostname = localAddress.address;
}
assert.equal(base.protocol, 'http:');
assert.ok(!['localhost', '127.0.0.1', '[::1]'].includes(base.hostname), 'LAN test must not use localhost');
const output = process.env.CURIO_TEST_OUTPUT || 'test-results';
fs.mkdirSync(output, { recursive: true });
async function verify(type, name) {
  const browser = await type.launch(name === 'chromium' && process.env.CURIO_BROWSER_EXECUTABLE ? { executablePath: process.env.CURIO_BROWSER_EXECUTABLE } : {});
  try {
    const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
    async function submit(button, expectedStatus) {
      const responsePromise = page.waitForResponse(response => response.request().method() === 'POST');
      await button.click();
      const response = await responsePromise;
      const headers = await response.request().allHeaders();
      assert.equal(headers.origin, base.origin, 'Form Origin must survive on HTTP LAN pages');
      assert.equal(headers['sec-fetch-site'], undefined, 'Exercise the Origin fallback without Fetch Metadata');
      assert.equal(response.status(), expectedStatus);
    }
    await page.goto(`${base.origin}/discover?kind=fact&id=octopus`);
    assert.equal(await page.evaluate(() => window.isSecureContext), false);
    await submit(page.getByRole('button', { name: 'Three hearts', exact: false }), 200);
    await page.getByText('You called it!', { exact: true }).waitFor();
    await page.goto(`${base.origin}/enter`);
    await page.getByLabel('Your username').fill(`lan-${name}`);
    await page.getByLabel('Household password', { exact: true }).fill(process.env.CURIO_TEST_PASSWORD || 'curio-local-qa-password');
    await submit(page.getByRole('button', { name: 'Let me in' }), 303);
    await page.waitForURL(`${base.origin}/`);
    await page.goto(`${base.origin}/discover?kind=fact&id=venus`);
    await submit(page.locator('.guess-option').first(), 303);
    await page.locator('.answer-reveal').waitFor();
    await submit(page.getByRole('button', { name: '♡ Keep this one', exact: true }), 303);
    await page.getByRole('status').waitFor();
    await page.screenshot({ path: `${output}/${name}-lan-reveal.png`, fullPage: true });
    await page.goto(`${base.origin}/settings`);
    await submit(page.getByRole('button', { name: 'Leave this profile' }), 303);
    await page.waitForURL(`${base.origin}/enter`);
    console.log(`${name}: HTTP LAN guest quiz, username entry, saved answer, favorite, and logout passed`);
  } finally { await browser.close(); }
}
(async () => {
  if (process.env.CURIO_TEST_BROWSER !== 'webkit') await verify(chromium, 'chromium');
  if (process.env.CURIO_TEST_BROWSER !== 'chromium') await verify(webkit, 'webkit');
})().catch(error => { console.error(error); process.exit(1); });

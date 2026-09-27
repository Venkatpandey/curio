'use strict';
// Account data never enters browser storage or the service worker cache.
for (const button of document.querySelectorAll('[data-toggle-password]')) {
  button.hidden = false;
  button.addEventListener('click', () => {
    const input = document.getElementById(button.dataset.togglePassword);
    const show = input.type === 'password';
    input.type = show ? 'text' : 'password';
    button.textContent = show ? 'Hide' : 'Show';
    button.setAttribute('aria-label', show ? 'Hide password' : 'Show password');
  });
}
if ('serviceWorker' in navigator && window.isSecureContext) {
  navigator.serviceWorker.register('/sw.js').catch(() => {
    // Ordinary navigation remains available if registration fails.
  });
}

// Check catalogue identity in the background; leave the current page untouched.
const edition = document.querySelector('[data-edition-day]');
if (edition) {
  const toggle = document.querySelector('[data-refresh-toggle]');
  const notice = document.querySelector('.freshness-notice');
  const message = document.querySelector('[data-freshness-message]');
  toggle.closest('label').hidden = false;
  try { toggle.checked = localStorage.getItem('curio:pause-refresh') !== 'true'; } catch (_) { /* Storage can be disabled. */ }
  let checking = false;
  let lastCheck = 0;
  async function checkFreshness() {
    if (!toggle.checked || document.hidden || checking || Date.now() - lastCheck < 60000) return;
    checking = true;
    lastCheck = Date.now();
    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 8000);
    try {
      const response = await fetch('/freshness', { cache: 'no-store', signal: controller.signal });
      if (!response.ok || !response.headers.get('content-type')?.includes('application/json')) return;
      const fresh = await response.json();
      if (fresh.day !== edition.dataset.editionDay || fresh.revision !== edition.dataset.editionRevision) {
        message.textContent = fresh.day !== edition.dataset.editionDay ? 'A new daily edition is ready.' : 'Your discovery collection has been updated.';
        notice.hidden = false;
      }
    } catch (_) {
      // Keep the cached page usable during a network interruption.
    } finally {
      clearTimeout(timeout);
      checking = false;
    }
  }
  toggle.addEventListener('change', () => {
    try { localStorage.setItem('curio:pause-refresh', String(!toggle.checked)); } catch (_) { /* Optional preference only. */ }
    if (toggle.checked) { lastCheck = 0; checkFreshness(); }
  });
  window.addEventListener('focus', checkFreshness);
  window.addEventListener('pageshow', checkFreshness);
  document.addEventListener('visibilitychange', checkFreshness);
  setInterval(checkFreshness, 5 * 60000);
}

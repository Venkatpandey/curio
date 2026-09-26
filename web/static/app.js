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

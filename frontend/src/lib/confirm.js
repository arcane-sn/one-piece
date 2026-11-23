// In-app replacement for window.confirm: `if (!(await confirmDialog('Delete it?', { danger: true }))) return`.
// Rendered by <Confirmer /> (mounted once in App).
export const confirmDialog = (message, opts = {}) =>
  new Promise((resolve) => window.dispatchEvent(new CustomEvent('op-confirm', { detail: { message, ...opts, resolve } })))

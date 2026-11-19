// Promise-based replacement for window.confirm, shown by <ConfirmHost />:
//   if (!(await confirmDialog({ title: 'Delete ticket?', message: '…', confirmLabel: 'Delete', danger: true }))) return
export const confirmDialog = (opts) =>
  new Promise((resolve) => window.dispatchEvent(new CustomEvent('op-confirm', { detail: { ...opts, resolve } })))

// Fire-and-forget notice: toast('Something went wrong'), toast('Saved', 'ok'). Shown by <Toaster />.
export const toast = (message, kind = 'error') => window.dispatchEvent(new CustomEvent('op-toast', { detail: { message, kind } }))

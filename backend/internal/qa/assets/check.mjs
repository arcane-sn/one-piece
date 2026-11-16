// Written by One Piece. Visits the session check URL with the saved login and prints {finalUrl, status}.
import { chromium } from '@playwright/test'

const [, , checkUrl, state, headed] = process.argv
const allowed = JSON.parse(process.env.QA_ALLOWED_HOSTS || '[]')
const ok = (h) => ['localhost', '127.0.0.1', '::1', '[::1]'].includes(h) || allowed.some((a) => a === h || (a.startsWith('*.') && h.endsWith(a.slice(1))))
const browser = await chromium.launch({ headless: headed !== '1' })
try {
  const ctx = await browser.newContext({ storageState: state })
  await ctx.route('**/*', (r) => { try { return ok(new URL(r.request().url()).hostname) ? r.continue() : r.abort() } catch { return r.continue() } })
  const page = await ctx.newPage()
  const resp = await page.goto(checkUrl, { timeout: 30000, waitUntil: 'load' })
  console.log(JSON.stringify({ finalUrl: page.url(), status: resp ? resp.status() : 0 }))
} catch (e) {
  console.log(JSON.stringify({ error: String(e.message || e).slice(0, 300) }))
} finally {
  await browser.close()
}

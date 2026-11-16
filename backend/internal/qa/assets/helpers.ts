// Written by One Piece. Specs import ONLY from './helpers'. Do not edit.
import { test as base, expect } from '@playwright/test'
import fs from 'fs'
import path from 'path'

const ATTEMPT = process.env.QA_ATTEMPT_DIR || '.'
const ALLOWED: string[] = JSON.parse(process.env.QA_ALLOWED_HOSTS || '[]')
const LOGIN = process.env.QA_LOGIN_URL || ''

function hostAllowed(h: string): boolean {
  h = h.toLowerCase().replace(/\.$/, '')
  if (h === 'localhost' || h === '127.0.0.1' || h === '[::1]' || h === '::1') return true
  return ALLOWED.some((a) => a === h || (a.startsWith('*.') && h.endsWith(a.slice(1)) && h.length > a.length - 1))
}

// Every page aborts requests to hosts outside the board's allowed list (a second layer behind the proxy).
export const test = base.extend({
  page: async ({ page }, use) => {
    await page.context().route('**/*', (route) => {
      let u: URL
      try { u = new URL(route.request().url()) } catch { return route.continue() }
      if (u.protocol !== 'http:' && u.protocol !== 'https:' && u.protocol !== 'ws:' && u.protocol !== 'wss:') return route.continue()
      if (hostAllowed(u.hostname)) return route.continue()
      try { fs.appendFileSync(path.join(ATTEMPT, 'blocked-routes.txt'), u.hostname.toLowerCase() + '\n') } catch {}
      return route.abort('blockedbyclient')
    })
    await use(page)
  },
})
export { expect }

let n = 0

// One test step that always ends with a screenshot, even when the step fails.
export async function qaStep(page: any, title: string, body: () => Promise<void>) {
  n++
  const pos = n
  const slug = title.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '').slice(0, 40) || 'step'
  const rel = `steps/${String(pos).padStart(2, '0')}-${slug}.png`
  const t0 = Date.now()
  let status = 'passed'
  let error = ''
  try {
    await test.step(title, body)
  } catch (e: any) {
    status = 'failed'
    error = String(e?.message || e).slice(0, 1500)
    throw e
  } finally {
    try {
      fs.mkdirSync(path.join(ATTEMPT, 'steps'), { recursive: true })
      await page.screenshot({ path: path.join(ATTEMPT, rel) })
    } catch {}
    let loginRedirect = false
    try { loginRedirect = !!LOGIN && page.url().startsWith(LOGIN) } catch {}
    const file = path.join(ATTEMPT, 'steps.json')
    let all: any[] = []
    try { all = JSON.parse(fs.readFileSync(file, 'utf8')) } catch {}
    all.push({ position: pos, title, status, screenshot: rel, durationMs: Date.now() - t0, error, loginRedirect })
    fs.writeFileSync(file, JSON.stringify(all))
  }
}

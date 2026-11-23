// Written by One Piece. Opens a visible browser; the owner logs in by hand and closes the window.
// The session is autosaved every 3 s, so the last save before closing is the session.
import { chromium } from '@playwright/test'
import fs from 'fs'

const [, , loginUrl, out] = process.argv
const tmp = out + '.tmp'
const browser = await chromium.launch({ headless: false })
const ctx = await browser.newContext()
const page = await ctx.newPage()
const save = async () => {
  try {
    fs.writeFileSync(tmp, JSON.stringify(await ctx.storageState()), { mode: 0o600 })
    fs.renameSync(tmp, out)
  } catch {}
}
const timer = setInterval(save, 3000)
page.on('framenavigated', () => setTimeout(save, 1500))
browser.on('disconnected', () => { clearInterval(timer); process.exit(0) })
// On macOS closing the last window leaves the browser app running, so end it when no page is left
const watch = (p) => p.on('close', async () => {
  if (ctx.pages().length === 0) { await save(); await browser.close().catch(() => {}); process.exit(0) }
})
ctx.pages().forEach(watch)
ctx.on('page', watch)
await page.goto(loginUrl).catch(() => {})

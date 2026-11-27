import { useEffect, useState } from 'react'
import { api, baseName } from '../api'
import { toast } from '../lib/toast'

// Parse a unified git diff into hunks with old/new line numbers (VS Code / Zed style gutters).
function parseDiff(text) {
  const rows = []
  let status = 'M', o = 0, n = 0, inHunk = false
  for (const l of text.split('\n')) {
    if (l.startsWith('@@')) {
      const m = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@ ?(.*)$/.exec(l)
      if (m) { o = +m[1]; n = +m[2]; inHunk = true; rows.push({ t: 'hunk', text: l.replace(/^(@@[^@]*@@).*$/, '$1') + (m[3] ? '  ' + m[3] : '') }) }
      continue
    }
    if (!inHunk) {
      if (l.startsWith('new file mode')) status = 'A'
      else if (l.startsWith('deleted file mode')) status = 'D'
      else if (l.startsWith('rename from')) status = 'R'
      continue
    }
    if (l.startsWith('+')) rows.push({ t: 'add', n: n++, text: l.slice(1) })
    else if (l.startsWith('-')) rows.push({ t: 'del', o: o++, text: l.slice(1) })
    else if (l.startsWith('\\')) rows.push({ t: 'note', text: l })
    else if (l !== '' || rows.length) rows.push({ t: 'ctx', o: o++, n: n++, text: l.slice(1) })
  }
  while (rows.length && rows[rows.length - 1].t === 'ctx' && rows[rows.length - 1].text === '') rows.pop()
  return { rows, status }
}

const splitPath = (p) => { const i = p.lastIndexOf('/'); return i < 0 ? ['', p] : [p.slice(0, i + 1), p.slice(i + 1)] }

// What an Agent Task committed: per repo the commit, its files with +/- and, on click, the file's diff.
export default function ChangesPanel({ itemId, refreshKey, done }) {
  const [changes, setChanges] = useState(null)
  const [open, setOpen] = useState(null) // `${changeId}:${path}`
  const [diff, setDiff] = useState({}) // key -> { text, truncated } | 'loading'
  useEffect(() => {
    api(`/api/items/${itemId}/changes`).then((r) => setChanges(r ?? [])).catch(() => setChanges([]))
  }, [itemId, refreshKey])
  if (!changes) return null
  if (changes.length === 0) return done ? <p className="hint">No commit recorded for this task: its repo had no feature branch when it ran (blocked or not registered), so the edits were not committed.</p> : null

  async function toggle(c, path) {
    const key = `${c.id}:${path}`
    setOpen((o) => (o === key ? null : key))
    if (diff[key]) return
    setDiff((d) => ({ ...d, [key]: 'loading' }))
    try {
      const r = await api(`/api/item-changes/${c.id}/diff?file=${encodeURIComponent(path)}`)
      setDiff((d) => ({ ...d, [key]: { text: r.diff, truncated: r.truncated } }))
    } catch (e) {
      setDiff((d) => { const n = { ...d }; delete n[key]; return n })
      toast(e.message)
    }
  }

  return (
    <div className="field changes">
      <span>Changes</span>
      {changes.map((c) => (
        <div className="change-repo" key={c.id}>
          <div className="change-head">
            <strong title={c.repo}>{baseName(c.repo)}</strong>
            <button type="button" className="link-btn" title="Copy the commit SHA" onClick={() => navigator.clipboard?.writeText(c.sha).then(() => toast('SHA copied', 'ok'))}>
              <code>{c.sha.slice(0, 8)}</code>
            </button>
            <span className="change-stat"><b className="add">+{c.additions}</b> <b className="del">−{c.deletions}</b> · {c.files.length} file{c.files.length === 1 ? '' : 's'}</span>
          </div>
          <ul className="change-files">
            {c.files.map((f) => {
              const key = `${c.id}:${f.path}`
              const d = diff[key]
              return (
                <li key={f.path}>
                  <button type="button" className="change-file" aria-expanded={open === key} onClick={() => toggle(c, f.path)}>
                    <span className="change-chev">{open === key ? '▾' : '▸'}</span>
                    <span className="change-name">{splitPath(f.path)[1]}</span>
                    <span className="change-dir" title={f.path}>{splitPath(f.path)[0]}</span>
                    <span className="change-stat">{f.additions < 0 ? 'binary' : <><b className="add">+{f.additions}</b> <b className="del">−{f.deletions}</b></>}</span>
                  </button>
                  {open === key && <DiffView d={d} />}
                </li>
              )
            })}
          </ul>
        </div>
      ))}
    </div>
  )
}

function DiffView({ d }) {
  if (!d || d === 'loading') return <div className="diff diff-msg">Loading…</div>
  const { rows } = parseDiff(d.text)
  if (rows.length === 0) return <div className="diff diff-msg">No textual changes (binary or mode change).</div>
  return (
    <div className="diff">
      {rows.map((r, i) => r.t === 'hunk'
        ? <div key={i} className="dl dl-hunk"><span className="dl-no" /><span className="dl-no" /><span className="dl-sign" /><span className="dl-code">{r.text}</span></div>
        : <div key={i} className={`dl dl-${r.t}`}>
            <span className="dl-no">{r.o ?? ''}</span><span className="dl-no">{r.n ?? ''}</span>
            <span className="dl-sign">{r.t === 'add' ? '+' : r.t === 'del' ? '−' : ''}</span>
            <span className="dl-code">{r.text || ' '}</span>
          </div>)}
      {d.truncated && <div className="dl dl-hunk"><span className="dl-no" /><span className="dl-no" /><span className="dl-sign" /><span className="dl-code">… diff cut at 200 KB; open the commit in git for the rest</span></div>}
    </div>
  )
}

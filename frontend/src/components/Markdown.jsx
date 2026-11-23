import { Fragment } from 'react'

// Small, dependency-free Markdown for Claude's replies: headings, lists, quotes, fenced and inline code,
// bold, italic and http(s)/mailto links. It builds React elements only (never raw HTML), so nothing in a
// reply can inject markup. Half-written text while a reply streams in degrades to plain text.
const INLINE = /(`[^`\n]+`|\*\*[^*\n]+\*\*|(?<!\w)__[^_\n]+__(?!\w)|\*[^*\s][^*\n]*\*|(?<!\w)_[^_\s][^_\n]*_(?!\w)|\[[^\]\n]+\]\((?:https?:\/\/|mailto:)[^)\s]+\))/g

function inline(text, key = '') {
  return text.split(INLINE).map((part, i) => {
    const k = key + i
    if (!part) return null
    if (i % 2 === 0) return <Fragment key={k}>{part}</Fragment>
    if (part[0] === '`') return <code key={k}>{part.slice(1, -1)}</code>
    if (part.startsWith('**') || part.startsWith('__')) return <strong key={k}>{inline(part.slice(2, -2), k)}</strong>
    if (part[0] === '*' || part[0] === '_') return <em key={k}>{inline(part.slice(1, -1), k)}</em>
    const m = /^\[([^\]]+)\]\(([^)]+)\)$/.exec(part)
    return <a key={k} href={m[2]} target="_blank" rel="noreferrer noopener">{inline(m[1], k)}</a>
  })
}

export default function Markdown({ text }) {
  const lines = text.replace(/\r\n?/g, '\n').split('\n')
  const out = []
  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    if (/^```/.test(line)) { // fenced code; an unclosed fence (still streaming) runs to the end
      const buf = []
      i++
      while (i < lines.length && !/^```/.test(lines[i])) buf.push(lines[i++])
      i++
      out.push(<pre key={out.length}><code>{buf.join('\n')}</code></pre>)
    } else if (/^\s*$/.test(line)) {
      i++
    } else if (/^#{1,4}\s/.test(line)) {
      const level = Math.min(line.match(/^#+/)[0].length + 1, 5)
      const H = 'h' + level
      out.push(<H key={out.length}>{inline(line.replace(/^#+\s*/, ''))}</H>)
      i++
    } else if (/^\s*([-*•]|\d+[.)])\s+/.test(line)) {
      const ordered = /^\s*\d/.test(line)
      const items = []
      while (i < lines.length && /^\s*([-*•]|\d+[.)])\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*([-*•]|\d+[.)])\s+/, ''))
        i++
      }
      const L = ordered ? 'ol' : 'ul'
      out.push(<L key={out.length}>{items.map((t, j) => <li key={j}>{inline(t, 'l' + j)}</li>)}</L>)
    } else if (/^>\s?/.test(line)) {
      const buf = []
      while (i < lines.length && /^>\s?/.test(lines[i])) buf.push(lines[i++].replace(/^>\s?/, ''))
      out.push(<blockquote key={out.length}>{inline(buf.join(' '))}</blockquote>)
    } else {
      const buf = []
      while (i < lines.length && lines[i].trim() && !/^(```|#{1,4}\s|>\s?|\s*([-*•]|\d+[.)])\s+)/.test(lines[i])) buf.push(lines[i++])
      out.push(<p key={out.length}>{buf.map((t, j) => <Fragment key={j}>{j > 0 && <br />}{inline(t, 'p' + j)}</Fragment>)}</p>)
    }
  }
  return <div className="md">{out}</div>
}

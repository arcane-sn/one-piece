// Swimlane DSL parser + SVG renderer, ported from HEPHAESTUS/swim-lanes (same syntax).
const SANS = '"Source Sans 3","Source Sans Pro",system-ui,-apple-system,sans-serif';
const MONO = '"Source Code Pro",ui-monospace,Menlo,monospace';
const FS = 12.5, HS = 13.5, TS = 24, LH = 17;   // font label, header, title, tinggi baris note
const SAMPLE = [
  'title: Login flow',
  'autonumber',
  '',
  'Browser -> API: POST /login',
  'API -> Auth: `verify(credentials)`',
  'Auth --> API: **ok** + user',
  'note: Token ditandatangani dengan *RS256*',
  'API -> API: issue JWT',
  'API --> Browser: 200 + token',
  '',
  '_: **Setelah login**',
  '',
  'if: token valid',
  '  Browser -> API: GET /me',
  '  API --> Browser: profil',
  'else: token kadaluarsa',
  '  Browser -x API: 401',
  'end',
  '',
  '...: user idle 30 menit',
  '',
  'Browser ->> API: refresh',
  'note Browser, Auth:',
  'Catatan bisa membentang di beberapa participant',
  '',
  '* poin satu',
  '* poin dua',
  '',
  'API => Browser: token baru',
  '',
  '// panah: ->  ->>  -->  -->>  <--  <->  -x  =>',
  '// pembatas: _: tipis   -: biasa   --: putus   =: tebal',
].join('\n');

/* ---------- teks kaya: bold, italic, strike, code, link ---------- */
const cv = typeof document === 'undefined' ? { measureText: (t) => ({ width: t.length * 6.5 }) } : document.createElement('canvas').getContext('2d');
const ICON = /\{fa[a-z]*-[^}]*\}/g;
const esc = s => s.replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
const TOK = /(`[^`]+`)|(\*\*.+?\*\*)|(~~.+?~~)|(\[[^\]]+\]\([^)]+\))|((?<![\w*])[*_][^*_]+?[*_](?![\w*]))/g;
function runs(s) {
  s = s.replace(ICON, '');
  const out = []; let last = 0;
  for (const m of s.matchAll(TOK)) {   // matchAll memakai salinan regex, aman untuk rekursi
    if (m.index > last) out.push({ t: s.slice(last, m.index) });
    const tok = m[0];
    if (m[1]) out.push({ t: tok.slice(1, -1), code: 1 });
    else if (m[2]) runs(tok.slice(2, -2)).forEach(r => out.push({ ...r, b: 1 }));
    else if (m[3]) runs(tok.slice(2, -2)).forEach(r => out.push({ ...r, s: 1 }));
    else if (m[4]) { const l = tok.match(/^\[([^\]]+)\]\(([^)]+)\)$/); out.push({ t: l[1], link: l[2] }); }
    else runs(tok.slice(1, -1)).forEach(r => out.push({ ...r, i: 1 }));
    last = m.index + tok.length;
  }
  if (last < s.length) out.push({ t: s.slice(last) });
  return out;
}
const meas = (r, size) => {
  cv.font = `${r.i ? 'italic ' : ''}${r.b ? 700 : 400} ${r.code ? size - 1 : size}px ${r.code ? MONO : SANS}`;
  return cv.measureText(r.t).width + (r.code ? 10 : 0);
};
const rw = (R, size = FS) => R.reduce((a, r) => a + meas(r, size), 0);
const tw = (s, bold, size = FS) => rw(runs(s).map(r => ({ ...r, b: r.b || bold })), size);
const plain = s => runs(s).map(r => r.t).join('');

// satu baris teks -> <text> per potongan; kode inline dapat kotak abu
function T(x, y, s, o = {}) {
  const size = o.size || FS, dd = o.ed != null ? ` data-ed="${o.ed}"` : '';
  const R = (Array.isArray(s) ? s : runs(s)).map(r => ({ ...r, b: r.b || o.b }));
  const ws = R.map(r => meas(r, size)), tot = ws.reduce((a, b) => a + b, 0);
  let cx = o.anchor === 'middle' ? x - tot / 2 : o.anchor === 'end' ? x - tot : x, out = '';
  R.forEach((r, i) => {
    if (r.code) out += `<rect x="${cx}" y="${y - size * 0.95}" width="${ws[i]}" height="${size * 1.4}" rx="3" class="chip"${dd}/>`;
    const a = `x="${cx + (r.code ? 5 : 0)}" y="${y}"` + (r.code ? ' class="c"' : '') + (r.b ? ' font-weight="700"' : '') +
      (r.i ? ' font-style="italic"' : '') + (r.s ? ' text-decoration="line-through"' : '') +
      (r.link ? ' fill="#0000ee" text-decoration="underline"' : o.fill ? ` fill="${o.fill}"` : '') + (o.size ? ` font-size="${size}"` : '') + dd;
    let node = `<text ${a} xml:space="preserve">${esc(r.t)}</text>`;
    if (r.link && /^https?:/i.test(r.link)) node = `<a href="${esc(r.link)}" target="_blank" rel="noopener">${node}</a>`;
    out += node; cx += ws[i];
  });
  return out;
}

// bungkus kata per kata tanpa memutus gaya bold/italic/kode
function wrap(text, max, size = FS) {
  const toks = [];
  runs(text).forEach(r => { if (r.code || r.link) toks.push(r); else r.t.split(/(\s+)/).filter(Boolean).forEach(w => toks.push({ ...r, t: w })); });
  const ws = t => /^\s+$/.test(t.t), lines = [[]]; let w = 0;
  for (const t of toks) {
    const x = meas(t, size), L = lines[lines.length - 1];
    if (ws(t)) { if (L.length) { L.push(t); w += x; } continue; }
    if (w + x > max && L.length) { while (L.length && ws(L[L.length - 1])) L.pop(); lines.push([t]); w = x; }
    else { L.push(t); w += x; }
  }
  const last = lines[lines.length - 1]; while (last.length && ws(last[last.length - 1])) last.pop();
  return lines;
}

// label pesan: dibungkus bila lebih lebar dari MSGW, jangan melebarkan kolom tanpa batas
const MSGW = 300, lw = L => L.reduce((s, t) => s + meas(t, FS), 0);

// isi note: paragraf, bullet (* + -), dan daftar bernomor
function noteLines(text, max) {
  const blocks = []; let cur = null, m;
  for (const raw of text.split('\n')) {
    const l = raw.trim();
    if (!l) { cur = null; continue; }
    if ((m = l.match(/^[*+-]\s+(.*)$/))) { blocks.push({ k: 'ul', t: m[1] }); cur = null; }
    else if ((m = l.match(/^(\d+)\.\s+(.*)$/))) { blocks.push({ k: 'ol', n: m[1], t: m[2] }); cur = null; }
    else if (cur) cur.t += ' ' + l;
    else { cur = { k: 'p', t: l }; blocks.push(cur); }
  }
  const out = []; let y = 0;
  blocks.forEach((b, i) => {
    const p = blocks[i - 1]; if (p) y += (p.k !== 'p' && b.k === p.k) ? 2 : 8;
    const ind = b.k === 'p' ? 0 : 16;
    wrap(b.t, max - ind).forEach((r, j) => { out.push({ r, y, x: ind, mark: j ? '' : b.k === 'ul' ? 'ul' : b.k === 'ol' ? b.n + '.' : '' }); y += LH; });
  });
  return { lines: out, h: y };
}
const nlWidth = nl => Math.max(0, ...nl.lines.map(l => l.x + rw(l.r)));

/* ---------- parser ---------- */
const OPS = '<->|-->>|->>|-->|<--|->|<-|-x|=>|<=';
const A1 = new RegExp(`^(.+?)\\s+(${OPS})\\s+(.+?)\\s*(?::\\s*(.*))?$`);
const A2 = /^([^:<>=]+?)\s*(<->|-->>|->>|-->|<--|->|<-|=>|<=)\s*([^:]+?)\s*(?::\s*(.*))?$/;
const DIV = { '_': 'thin', '-': 'regular', '--': 'dashed', '=': 'bold' };
const KW = /^(title|order|group|if|else|\.\.\.|--|-|=|_)\s*:/i, NOTE = /^note\s*[^:<>=]*:/i;
const isStmt = l => KW.test(l) || NOTE.test(l) || /^(end|autonumber)$/i.test(l) || l.startsWith('//') || A1.test(l) || A2.test(l);

// lanjutan note: berhenti di blank line ganda, di statement setelah blank, atau di akhir teks
function noteBody(lines, from) {
  const body = []; let j = from;
  while (j < lines.length) {
    const t = lines[j].trim();
    if (!t) {
      let k = j; while (k < lines.length && !lines[k].trim()) k++;
      if (k - j >= 2 || k >= lines.length || isStmt(lines[k].trim())) break;
      body.push(''); j++; continue;
    }
    if (!body.length && isStmt(t)) break;
    body.push(t); j++;
  }
  return { text: body.join('\n'), end: j - 1 };
}

// posisi teks (setelah ':') di baris sumber, supaya bisa diganti lewat edit di diagram
const tailEd = (l, raw, i, tail) => {
  const end = raw.indexOf(l) + l.length;
  return { kind: 'text', line: i, s: end - tail.length, e: end, pre: tail ? '' : /:$/.test(l) ? ' ' : ': ' };
};

function parse(src) {
  const parts = [], ev = [], bad = [], noteRows = new Set();
  let title = '', titleEd = null, auto = false, order = [], nmsg = 0, last = null, skip = -1;
  const add = n => { n = n.trim(); if (!parts.includes(n)) parts.push(n); return n; };
  const lines = src.split('\n');
  lines.forEach((raw, i) => {
    if (i <= skip) return;
    const n0 = ev.length;
    ((raw, i) => {   // tiap event dicatat rentang baris sumbernya (untuk highlight)
    const l = raw.trim(); let m;
    if (!l || l.startsWith('//') || l.startsWith('#')) return;
    if (/^autonumber$/i.test(l)) { auto = true; return; }
    if (/^end$/i.test(l)) { ev.push({ t: 'end' }); return; }
    if ((m = l.match(/^note\s*([^:<>=]*?)\s*:\s*(.*)$/i))) {
      const names = m[1] ? m[1].split(',').map(add) : last ? [last.from, last.to] : [];
      const b = noteBody(lines, i + 1);
      const text = (m[2] ? m[2] + '\n' + b.text : b.text).trim();
      for (let r = i; r <= b.end; r++) noteRows.add(r);
      skip = b.end;
      ev.push({ t: 'note', names, text, ed: { kind: 'note', line: i, end: b.end, head: l.slice(0, l.indexOf(':') + 1) } }); return;
    }
    if ((m = l.match(/^(title|order|group|if|else|\.\.\.|--|-|=|_)\s*:\s*(.*)$/i))) {
      const k = m[1].toLowerCase(), ed = tailEd(l, raw, i, m[2]);
      if (k === 'title') { title = m[2]; titleEd = ed; }
      else if (k === 'order') order = m[2].split(',').map(s => s.trim()).filter(Boolean);
      else if (k === 'group' || k === 'if') ev.push({ t: 'open', kind: k, text: m[2], ed });
      else if (k === 'else') ev.push({ t: 'else', text: m[2], ed });
      else if (k === '...') ev.push({ t: 'delay', text: m[2], ed });
      else ev.push({ t: 'div', style: DIV[k], text: m[2], ed });
      return;
    }
    if ((m = l.match(A1) || l.match(A2))) {
      let [, a, op, b, text] = m; a = add(a); b = add(b);
      if (op[0] === '<' && op !== '<->') [a, b] = [b, a];
      nmsg++;
      last = { t: 'msg', from: a, to: b, dash: op.includes('--'), open: op.includes('>>'), lost: op === '-x', bold: op === '=>' || op === '<=', both: op === '<->', text: text || '', ed: tailEd(l, raw, i, text || '') };
      ev.push(last);
      return;
    }
    bad.push(i + 1);
    })(raw, i);
    for (let q = n0; q < ev.length; q++) { ev[q].l0 = i; ev[q].l1 = Math.max(i, skip); }
  });
  order.forEach(add);
  const final = [...order, ...parts.filter(p => !order.includes(p))];
  return { parts: final, ev, title, titleEd, auto, bad, nmsg, noteRows };
}

/* ---------- edit teks sumber dari diagram ---------- */
const escRe = s => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
// mengembalikan teks sumber baru; teks yang sama bila edit tidak valid
function editSource(text, ed, val, noteRows = new Set()) {
  const lines = text.split('\n');
  if (ed.kind === 'name') {
    val = val.trim();
    if (!val || /[:,]/.test(val) || val === ed.raw) return text;
    const re = new RegExp(`(^|[\\s,])${escRe(ed.raw)}(?=[\\s,:<>=-]|$)`, 'g');
    const sub = s => s.replace(re, (_, p) => p + val);
    lines.forEach((line, i) => {
      const t = line.trim(); let m;
      if (!t || /^(\/\/|#)/.test(t)) return;
      if (noteRows.has(i) && !/^note\b/i.test(t)) return;
      if (/^order\s*:/i.test(t)) lines[i] = sub(line);
      else if (/^note\b[^:<>=]*:/i.test(t)) { const c = line.indexOf(':'); lines[i] = sub(line.slice(0, c)) + line.slice(c); }
      else if ((m = t.match(A1) || t.match(A2))) { const cut = line.indexOf(t) + t.length - (m[4] ?? '').length; lines[i] = sub(line.slice(0, cut)) + line.slice(cut); }
    });
  } else if (ed.kind === 'note') {
    const v = val.trim();
    const repl = v.includes('\n') ? [ed.head, ...v.split('\n')] : [v ? ed.head + ' ' + v : ed.head];
    const next = lines[ed.end + 1];
    if (repl.length > 1 && next !== undefined && next.trim()) repl.push('');   // pisahkan dari statement berikutnya, kalau tidak ikut tertelan jadi isi note
    lines.splice(ed.line, ed.end - ed.line + 1, ...repl);
  } else {
    const line = lines[ed.line];
    lines[ed.line] = line.slice(0, ed.s) + (ed.s === ed.e && val ? ed.pre : '') + val + line.slice(ed.e);
  }
  return lines.join('\n');
}

/* ---------- renderer ---------- */
const STYLE = `<style>
text{font-family:${SANS};font-size:${FS}px;fill:#222;white-space:pre}
.c{font-family:${MONO};font-size:${FS - 1}px}
.chip{fill:#f1efe9;stroke:#d9d6cc}
.ti{font-size:${TS}px;font-weight:600;text-anchor:middle;letter-spacing:-.01em}
.hd{fill:#fff;stroke:#a8a8a8;stroke-width:1.6}.ht{font-size:${HS}px;font-weight:600;text-anchor:middle}
.ll{stroke:#a3a3a3;stroke-width:1.3}
.ar{stroke:#222;stroke-width:1.3;fill:none}.ar.ds{stroke-dasharray:5 4}.ar.bd{stroke-width:2.6}
.nt{fill:#fffdee;stroke:#d8d4bd}
.hlr{fill:#f7eef1}
.dl{stroke:#9a9a9a;stroke-width:1.3}.dl.thin{stroke:#bdbab2;stroke-width:1}.dl.dashed{stroke-dasharray:6 4}
.db{fill:#ebe9e3}
.dot{fill:#8a877f}
.fr{fill:none;stroke:#bdbab2;stroke-width:1.1}.tab{fill:#ebe9e3}
.el{stroke:#bdbab2;stroke-width:1.1;stroke-dasharray:5 4}
</style>`;
const SH = { ah: 4.8, ahb: 6.3, ao: 0 };

function build(P, minW = 0) {
  const { parts, ev, title, auto } = P, n = parts.length;
  if (!n) return null;
  const idx = p => parts.indexOf(p);
  const hw = parts.map(p => Math.max(100, tw(p, 1, HS) + 40));
  const gap = []; for (let i = 0; i < n - 1; i++) gap.push((hw[i] + hw[i + 1]) / 2 + 20);
  const edge = Array(n).fill(0); let selfX = 0;
  const need = (a, b, w) => {
    if (a > b) [a, b] = [b, a];
    if (a === b) { if (a < n - 1) gap[a] = Math.max(gap[a], w); else selfX = Math.max(selfX, w); return; }
    let cur = 0; for (let i = a; i < b; i++) cur += gap[i];
    if (cur < w) { const add = (w - cur) / (b - a); for (let i = a; i < b; i++) gap[i] += add; }
  };
  let k = 0;
  for (const e of ev) {
    if (e.t === 'msg') {
      k++; e.label = (auto ? k + '. ' : '') + e.text;
      e.lines = e.label.split('\\n').flatMap(s => wrap(s, MSGW));   // "\n" di sumber = baris baru manual (Shift+Enter saat edit)
      const w = Math.max(0, ...e.lines.map(lw));
      need(idx(e.from), idx(e.to), e.from === e.to ? Math.max(64, w + 26) : w + 48);
    } else if (e.t === 'note' && e.names.length) {
      const ids = e.names.map(idx), a = Math.min(...ids), b = Math.max(...ids);
      if (a === b) { e.nl = noteLines(e.text, 230); e.w = nlWidth(e.nl) + 24; edge[a] = Math.max(edge[a], e.w / 2); }
    }
  }
  let L = Math.max(hw[0] / 2 + 24, edge[0] + 16, 36), R = Math.max(hw[n - 1] / 2 + 24, edge[n - 1] + 16, selfX + 20);
  const titleW = title ? tw(plain(title), 1, TS) + 56 : 0;
  const nat = L + gap.reduce((s, g) => s + g, 0) + R;
  const target = Math.max(minW, titleW, nat);
  if (target > nat) {
    const extra = target - nat;
    if (n > 1) { for (let i = 0; i < n - 1; i++) gap[i] += extra / n; L += extra / (2 * n); R += extra / (2 * n); }
    else { L += extra / 2; R += extra / 2; }
  }
  const x = [L]; for (let i = 1; i < n; i++) x.push(x[i - 1] + gap[i - 1]);
  const W = Math.max(x[n - 1] + R, target);
  const avg = n > 1 ? (x[n - 1] - x[0]) / (n - 1) : 180;
  const bw = parts.map((_, i) => {
    const lim = Math.min(i > 0 ? gap[i - 1] : 1e9, i < n - 1 ? gap[i] : 1e9) - 14;
    // kotak di ujung tidak boleh lebih lebar dari margin kiri/kanan, kalau tidak terpotong tepi SVG
    const m = Math.min(i === 0 ? 2 * L - 12 : 1e9, i === n - 1 ? 2 * R - 12 : 1e9);
    return Math.max(hw[i], Math.min(avg * 0.7, 300, lim, m));
  });

  const top = title ? 68 : 24, HH = 40;
  let y = top + HH + 10, frames = '', body = '';
  const stack = [], rows = [], eds = [];
  const titleId = P.titleEd ? eds.push({ ...P.titleEd, raw: title }) - 1 : null;
  const nameIds = parts.map(p => eds.push({ kind: 'name', raw: p }) - 1);
  const tab = (x0, y0, label, ed) => { const w = tw(label, 1, 11) + 16; return [`<rect x="${x0}" y="${y0}" width="${w}" height="19" rx="4" class="tab"/>` + T(x0 + 8, y0 + 13.5, label, { b: 1, size: 11, fill: '#3a3833', ed }), w]; };
  const closeFrame = () => {
    const f = stack.pop(); y += 12;
    const x1 = 10 + f.d * 10, x2 = W - 10 - f.d * 10;
    frames += `<rect x="${x1}" y="${f.y0}" width="${x2 - x1}" height="${y - f.y0}" rx="4" class="fr"/>`;
    const head = (label, text, y0, ed, tabEd) => { const [s, w] = tab(x1, y0, label, tabEd); frames += s; if (text) frames += T(x1 + w + 8, y0 + 13.5, text, { ed }); };
    f.kind === 'if' ? head('if', f.text, f.y0, f.ed) : head(plain(f.text) || 'group', '', f.y0, null, f.ed);
    for (const s of f.elses) { frames += `<line x1="${x1}" y1="${s.y}" x2="${x2}" y2="${s.y}" class="el"/>`; head('else', s.text, s.y, s.ed); }
  };

  for (const e of ev) {
    const ys = y;
    if (e.ed) e.edId = eds.push({ ...e.ed, raw: e.text, nl: e.t === 'msg' }) - 1;   // nl: label pesan boleh multi-baris
    if (e.t === 'msg') {
      const ex = (e.lines.length - 1) * LH, a = x[idx(e.from)], b = x[idx(e.to)], ay = y + 22 + ex;   // ex: tinggi baris label tambahan
      const lab = (cx, o) => e.lines.map((l, j) => T(cx, ay - 7 - (e.lines.length - 1 - j) * LH, l, { ...o, ed: e.edId })).join('');
      const cls = 'ar' + (e.bold ? ' bd' : '') + (e.dash ? ' ds' : '');
      const id = e.bold ? 'ahb' : e.open ? 'ao' : 'ah', sh = SH[id], mk = `url(#${id})`;
      if (a === b) {
        const r = 4;
        body += `<path d="M${a} ${ay} H${a + 36 - r} A${r} ${r} 0 0 1 ${a + 36} ${ay + r} V${ay + 22 - r} A${r} ${r} 0 0 1 ${a + 36 - r} ${ay + 22} H${a + sh}" class="${cls}" stroke-linejoin="round" marker-end="${mk}"/>` +
          lab(a + 8, {});
        y += 52 + ex;
      } else {
        const dir = Math.sign(b - a);
        const x1 = e.both ? a + dir * sh : a;
        const x2 = e.lost ? b - dir * 14 : b - dir * sh;
        body += `<line x1="${x1}" y1="${ay}" x2="${x2}" y2="${ay}" class="${cls}"` +
          (e.lost ? '' : ` marker-end="${mk}"`) + (e.both ? ` marker-start="${mk}"` : '') + '/>';
        if (e.lost) body += `<path d="M${b - dir * 9 - 4} ${ay - 4}l8 8m0-8l-8 8" class="ar"/>`;
        body += lab((a + b) / 2, { anchor: 'middle' });
        y += 38 + ex;
      }
    } else if (e.t === 'note') {
      let x1, x2, nl;
      if (!e.names.length) { x1 = x[0] - bw[0] / 2; x2 = x[n - 1] + bw[n - 1] / 2; nl = noteLines(e.text, x2 - x1 - 24); }
      else {
        const ids = e.names.map(idx), a = Math.min(...ids), b = Math.max(...ids);
        if (a === b) { nl = e.nl; x1 = x[a] - e.w / 2; x2 = x[a] + e.w / 2; }
        else { x1 = x[a] - 22; x2 = x[b] + 22; nl = noteLines(e.text, x2 - x1 - 24); }
      }
      const h = nl.h + 16, ny = y + 4;
      body += `<rect x="${x1}" y="${ny}" width="${x2 - x1}" height="${h}" class="nt" data-ed="${e.edId}"/>`;
      for (const l of nl.lines) {
        const by = ny + 20 + l.y;
        if (l.mark === 'ul') body += `<circle cx="${x1 + 16}" cy="${by - 4}" r="1.9" fill="#222"/>`;
        else if (l.mark) body += T(x1 + 12, by, l.mark);
        body += T(x1 + 12 + l.x, by, l.r, { ed: e.edId });
      }
      y += h + 22;
    } else if (e.t === 'div') {
      if (e.style === 'thin') {
        const ly = y + 26;
        body += `<line x1="20" y1="${ly}" x2="${W - 20}" y2="${ly}" class="dl thin"/>` + (e.text ? T(W / 2, ly - 7, e.text, { anchor: 'middle', b: 1, ed: e.edId }) : '');
        y += 40;
      } else if (e.style === 'bold') {
        const ly = y + 19;
        body += `<rect x="0" y="${ly - 13}" width="${W}" height="26" class="db"/>` + (e.text ? T(W / 2, ly + 4, e.text, { anchor: 'middle', b: 1, ed: e.edId }) : '');
        y += 38;
      } else {
        const ly = y + 19;
        body += `<line x1="20" y1="${ly}" x2="${W - 20}" y2="${ly}" class="dl ${e.style}"/>`;
        if (e.text) { const w = tw(e.text, 1) + 22; body += `<rect x="${W / 2 - w / 2}" y="${ly - 9}" width="${w}" height="18" fill="#fff"/>` + T(W / 2, ly + 4, e.text, { anchor: 'middle', b: 1, fill: '#57544d', ed: e.edId }); }
        y += 38;
      }
    } else if (e.t === 'delay') {
      for (const px of x) body += `<rect x="${px - 4}" y="${y + 2}" width="8" height="32" fill="#fff"/>` + [8, 17, 26].map(d => `<circle cx="${px}" cy="${y + d}" r="1.6" class="dot"/>`).join('');
      if (e.text) { const w = tw(e.text, 1) + 26; body += `<rect x="${W / 2 - w / 2}" y="${y + 8}" width="${w}" height="20" rx="10" fill="#fff" stroke="#e2dfd8"/>` + T(W / 2, y + 22, e.text, { anchor: 'middle', b: 1, fill: '#57544d', ed: e.edId }); }
      y += 40;
    } else if (e.t === 'open') {
      stack.push({ y0: y, kind: e.kind, text: e.text, ed: e.edId, d: stack.length, elses: [] }); y += 30;
    } else if (e.t === 'else') {
      if (stack.length) { stack[stack.length - 1].elses.push({ y: y + 4, text: e.text, ed: e.edId }); y += 30; }
    } else if (e.t === 'end') {
      if (stack.length) closeFrame();
    }
    if (e.t !== 'end') rows.push({ l0: e.l0, l1: e.l1, y0: ys, y1: y - 4 });
  }
  while (stack.length) closeFrame();

  const yb = y + 6, H = yb + HH + 24;
  const lifelines = parts.map((p, i) => `<line x1="${x[i]}" y1="${top + HH}" x2="${x[i]}" y2="${yb}" class="ll"/>`).join('');
  const head = yy => parts.map((p, i) => `<rect x="${x[i] - bw[i] / 2}" y="${yy}" width="${bw[i]}" height="${HH}" rx="5" class="hd" data-ed="${nameIds[i]}"/><text x="${x[i]}" y="${yy + HH / 2 + 4.5}" class="ht" data-ed="${nameIds[i]}">${esc(p)}</text>`).join('');
  const t = title ? `<text x="${W / 2}" y="38" class="ti" data-ed="${titleId}">${esc(plain(title))}</text>` : '';
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" role="img" aria-label="${esc(plain(title) || 'Diagram')}">${STYLE}` +
    `<defs>` +
    `<marker id="ah" viewBox="0 0 10 10" refX="4" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M0 1L10 5L0 9z" fill="#222"/></marker>` +
    `<marker id="ahb" viewBox="0 0 10 10" refX="4" refY="5" markerWidth="10.5" markerHeight="10.5" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M0 1L10 5L0 9z" fill="#222"/></marker>` +
    `<marker id="ao" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="8" markerHeight="8" markerUnits="userSpaceOnUse" orient="auto-start-reverse"><path d="M1 1L9 5L1 9" fill="none" stroke="#222" stroke-width="1.4"/></marker>` +
    `</defs><rect width="100%" height="100%" fill="#fff"/><rect id="hr" x="0" y="0" width="${W}" height="0" class="hlr"/>${t}${frames}${lifelines}${body}${head(top)}${head(yb)}</svg>`;
  return { svg, w: W, h: H, rows, eds };
}

/* ---------- highlight editor ---------- */
const ARROW_HL = new RegExp(`^(\\s*)(.+?)(\\s+)(${OPS})(\\s+)(.+?)(\\s*)(?::(.*))?$`);
function hl(line, inNote) {
  let m;
  if (/^\s*(\/\/|#)/.test(line)) return `<i class="cm">${esc(line)}</i>`;
  if (inNote) return `<i class="nb">${esc(line)}</i>`;
  if ((m = line.match(/^(\s*)(title|order|group|if|else|\.\.\.|--|-|=|_)(\s*:)(.*)$/i)))
    return esc(m[1]) + `<i class="kw">${esc(m[2] + m[3])}</i>` + esc(m[4]);
  if (/^\s*(end|autonumber)\s*$/i.test(line)) return `<i class="kw">${esc(line)}</i>`;
  if ((m = line.match(ARROW_HL)))
    return esc(m[1]) + esc(m[2]) + esc(m[3]) + `<i class="op">${esc(m[4])}</i>` + esc(m[5]) + esc(m[6]) + esc(m[7]) + (m[8] !== undefined ? ':' + esc(m[8]) : '');
  return esc(line);
}

export { parse, build, hl, plain }

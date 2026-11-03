import { useState } from 'react'

// A draggable kanban card; click to edit title/description inline.
export default function CardItem({ card, onSave, onDelete, onDropOn }) {
  const [editing, setEditing] = useState(false)
  const [title, setTitle] = useState(card.title)
  const [desc, setDesc] = useState(card.description)

  async function save(e) {
    e.preventDefault()
    if (await onSave(card.id, title, desc)) setEditing(false)
  }
  function cancel() {
    setTitle(card.title)
    setDesc(card.description)
    setEditing(false)
  }

  if (editing) {
    return (
      <form className="kcard form" onSubmit={save} style={{ cursor: 'default' }}>
        <input autoFocus value={title} onChange={(e) => setTitle(e.target.value)} />
        <textarea rows={3} value={desc} onChange={(e) => setDesc(e.target.value)} />
        <div className="row">
          <button className="primary" type="submit" disabled={!title.trim()}>Simpan</button>
          <button type="button" onClick={cancel}>Batal</button>
        </div>
      </form>
    )
  }
  return (
    <div
      className="kcard"
      draggable
      onDragStart={(e) => e.dataTransfer.setData('text/plain', String(card.id))}
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDropOn}
      onClick={() => setEditing(true)}
    >
      <div className="row">
        <strong>{card.title}</strong>
        <button className="danger" title="Hapus" onClick={(e) => { e.stopPropagation(); onDelete(card) }}>×</button>
      </div>
      {card.description && <p>{card.description}</p>}
    </div>
  )
}

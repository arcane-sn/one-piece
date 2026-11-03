import { useState } from 'react'
import CardItem from './CardItem'
import Logo from './Logo'

// One kanban column. Dropping on a card inserts before it; dropping on the
// column itself appends to the end.
export default function Column({ status, label, dot, cards, onMove, onOpen, footer }) {
  const [over, setOver] = useState(false)

  function drop(e, index) {
    e.preventDefault()
    e.stopPropagation()
    setOver(false)
    const id = Number(e.dataTransfer.getData('text/plain'))
    if (id) onMove(id, status, index)
  }

  const [emptyTitle, emptyHint] =
    status === 'business' ? ['No cards yet', 'Add one to get started.'] : ['Nothing here yet', 'Drop a card to review it.']

  return (
    <section
      className={'column' + (over ? ' over' : '')}
      onDragOver={(e) => { e.preventDefault(); setOver(true) }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => drop(e, cards.length)}
    >
      <div className="col-head">
        <span className="col-title"><span className="dot" style={{ background: dot }} />{label}</span>
        <span className="col-count">{cards.length}</span>
      </div>
      {cards.map((c, i) => (
        <CardItem key={c.id} card={c} onOpen={onOpen} onDropOn={(e) => drop(e, i)} />
      ))}
      {cards.length === 0 && (
        <div className="col-empty">
          <Logo glyph size={32} />
          <b>{emptyTitle}</b>
          <span>{emptyHint}</span>
        </div>
      )}
      {footer}
    </section>
  )
}

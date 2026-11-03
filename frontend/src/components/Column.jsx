import { useState } from 'react'
import CardItem from './CardItem'

// One kanban column. Dropping on a card inserts before it; dropping on the
// column itself appends to the end.
export default function Column({ status, label, cards, onMove, onSave, onDelete, footer }) {
  const [over, setOver] = useState(false)

  function drop(e, index) {
    e.preventDefault()
    e.stopPropagation()
    setOver(false)
    const id = Number(e.dataTransfer.getData('text/plain'))
    if (id) onMove(id, status, index)
  }

  return (
    <section
      className={'column' + (over ? ' over' : '')}
      onDragOver={(e) => { e.preventDefault(); setOver(true) }}
      onDragLeave={() => setOver(false)}
      onDrop={(e) => drop(e, cards.length)}
    >
      <h3>{label} · {cards.length}</h3>
      {cards.map((c, i) => (
        <CardItem key={c.id} card={c} onSave={onSave} onDelete={onDelete} onDropOn={(e) => drop(e, i)} />
      ))}
      {footer}
    </section>
  )
}

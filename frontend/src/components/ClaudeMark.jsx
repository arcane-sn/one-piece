// Claude-style spark: a burst of rounded rays in Claude orange. A drawn approximation, not the official asset.
const LEN = [10, 7, 9, 6.5, 10, 7.5, 9, 6.5, 10, 7, 9.5, 6.5]
const RAYS = LEN.map((len, i) => {
  const a = (i * 30 - 90) * (Math.PI / 180), r0 = 3.2
  return [12 + Math.cos(a) * r0, 12 + Math.sin(a) * r0, 12 + Math.cos(a) * len, 12 + Math.sin(a) * len]
})

export default function ClaudeMark({ size = 14, color = '#D97757' }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" style={{ flex: 'none' }}>
      {RAYS.map(([x1, y1, x2, y2], i) => (
        <line key={i} x1={x1.toFixed(2)} y1={y1.toFixed(2)} x2={x2.toFixed(2)} y2={y2.toFixed(2)} stroke={color} strokeWidth="2.6" strokeLinecap="round" />
      ))}
    </svg>
  )
}

// Opens the Cmd+K palette (see CommandPalette).
export default function SearchButton() {
  return (
    <button type="button" className="search-btn" aria-label="Search" onClick={() => window.dispatchEvent(new Event('op:cmdk'))}>
      <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" /></svg>
      <span>Search</span>
      <kbd>⌘K</kbd>
    </button>
  )
}

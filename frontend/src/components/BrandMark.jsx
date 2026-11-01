export default function BrandMark({ compact = false, light = false }) {
  return (
    <span className={`brand-mark${compact ? ' is-compact' : ''}${light ? ' is-light' : ''}`}>
      <svg className="brand-mark-icon" viewBox="0 0 32 32" aria-hidden="true">
        <path d="M7 19v-6M13 24V8M19 20v-8M25 23V9" />
      </svg>
      {!compact && <span>livepoll</span>}
    </span>
  );
}

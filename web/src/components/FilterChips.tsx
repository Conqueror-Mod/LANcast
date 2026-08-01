import "./FilterChips.css";

export interface ChipOption {
  value: string;
  label: string;
}

// A labelled group of toggle chips for one filter facet. Selection is
// multi-value (Plex-style): each chip toggles independently, and the parent owns
// the state (it lives in the URL). Rendered only when there is something to pick.
export function FilterChips({
  label,
  options,
  selected,
  onToggle,
}: {
  label: string;
  options: ChipOption[];
  selected: Set<string>;
  onToggle: (value: string) => void;
}) {
  if (options.length === 0) return null;
  return (
    <div className="chips">
      <span className="chips__label">{label}</span>
      <div className="chips__row">
        {options.map((o) => {
          const on = selected.has(o.value);
          return (
            <button
              key={o.value}
              type="button"
              className={"chip" + (on ? " is-on" : "")}
              aria-pressed={on}
              onClick={() => onToggle(o.value)}
            >
              {o.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

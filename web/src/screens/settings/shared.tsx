import { RATING_RUNGS } from "@/lib/ratings";

// The rungs, shared with the peer-sharing screen so the two cannot drift
// apart (ADR 0071 §6).
export const RATING_CEILINGS = RATING_RUNGS;

export function RuleSelect({
  title,
  sub,
  value,
  options,
  onChange,
  disabled,
}: {
  title: string;
  sub: string;
  value: number;
  options: { value: number; label: string }[];
  onChange: (v: number) => void;
  disabled?: boolean;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{title}</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        <select
          className="set-select"
          value={value}
          disabled={disabled}
          onChange={(e) => onChange(Number(e.target.value))}
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>
    </div>
  );
}

/*
 * A server rule that is a number in a range.
 *
 * Committed on blur and on Enter rather than per keystroke: saving "1" on the
 * way to typing "16" is a write that means nothing, and with continue_weeks it
 * is a write that briefly empties somebody's shelf. Out-of-range input is put
 * back rather than sent — the server rejects it anyway, and a field that snaps
 * back says so faster than a toast.
 */
export function RuleNumber({
  title,
  sub,
  value,
  min,
  max,
  onCommit,
}: {
  title: string;
  sub: string;
  value: number;
  min: number;
  max: number;
  onCommit: (v: number) => void;
}) {
  return (
    <div className="set-row">
      <div className="set-row__main">
        <div className="set-row__title">{title}</div>
        <div className="set-row__sub">{sub}</div>
      </div>
      <div className="set-row__actions">
        <input
          className="set-input"
          type="number"
          min={min}
          max={max}
          // Uncontrolled, keyed on the server's value: the field is the user's
          // while they are typing in it, and the server's again once they are
          // done. A controlled input here fights every keystroke.
          key={value}
          defaultValue={value}
          onKeyDown={(e) => {
            if (e.key === "Enter") (e.target as HTMLInputElement).blur();
          }}
          onBlur={(e) => {
            const v = Number(e.target.value);
            if (Number.isInteger(v) && v >= min && v <= max && v !== value) {
              onCommit(v);
            } else {
              e.target.value = String(value);
            }
          }}
        />
      </div>
    </div>
  );
}

/*
 * groupFingerprint breaks a pin into readable runs.
 *
 * Base64 in one unbroken run is unreadable at the only moment it matters: two
 * people on the phone, one of them reading it to the other. Grouped, it can be
 * read aloud and lost place in without starting over. The server's own client
 * groups it identically, so the two screens being compared look alike.
 */
export function groupFingerprint(pin: string): string {
  return (pin.match(/.{1,8}/g) ?? [pin]).join(" ");
}

import { TILE_STEPS, useTileSize } from "@/lib/tileSize";
import "./TileSizeSlider.css";

/*
 * The grid density control, top-right of a library header.
 *
 * A native `input[type=range]`, styled — not a custom widget built from divs.
 * The keyboard model (ADR 0004) means every control has to answer to arrow
 * keys, Home and End, and report its value to a screen reader; a range input
 * does all of that in the platform, and a hand-rolled one is where that support
 * gets re-implemented badly.
 *
 * The value is a step index rather than a pixel width, so the notches are the
 * thing being chosen. `aria-valuetext` says the size in plain words, because
 * "3 of 6" is not what anybody is choosing between.
 */
const LABELS = ["Smallest", "Smaller", "Default", "Larger", "Large", "Largest"];

export function TileSizeSlider() {
  const [step, setStep] = useTileSize();
  return (
    <div className="tile-size">
      <TileGlyph size={9} />
      <input
        className="tile-size__range"
        type="range"
        min={0}
        max={TILE_STEPS.length - 1}
        step={1}
        value={step}
        onChange={(e) => setStep(Number(e.target.value))}
        aria-label="Tile size"
        aria-valuetext={LABELS[step] ?? `Step ${step + 1}`}
        title={`Tile size: ${LABELS[step] ?? step + 1}`}
      />
      <TileGlyph size={14} />
    </div>
  );
}

// Two squares at two sizes, drawn rather than typed: the ends of the slider say
// which direction is bigger without adding words to a header that already
// carries a name, a count and a search field.
function TileGlyph({ size }: { size: number }) {
  return (
    <span
      className="tile-size__glyph"
      style={{ width: size, height: size }}
      aria-hidden="true"
    />
  );
}

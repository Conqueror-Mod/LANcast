import { describe, expect, it } from "vitest";
import { toneStyle } from "./heroTone";

describe("toneStyle", () => {
  it("leaves the tint as it was when there is nothing to lean toward", () => {
    expect(toneStyle(undefined)).toBeUndefined();
    expect(toneStyle({ violet: 0, blue: 0, teal: 0, ember: 0, light: 0.4 })).toBeUndefined();
  });

  it("weights each wisp by the picture, within bounds", () => {
    expect(toneStyle({ violet: 0.1, blue: 0.1, teal: 0.05, ember: 0.75, light: 0.5 })).toEqual({
      "--tone-violet": "0.10",
      "--tone-blue": "0.10",
      "--tone-teal": "0.05",
      "--tone-ember": "0.75",
    });
    // A weight outside 0–1 cannot push a wisp past the stylesheet's bounds.
    expect(toneStyle({ violet: 3, blue: -1, teal: 0, ember: 0, light: 0 })?.["--tone-violet" as never]).toBe("1.00");
  });
});

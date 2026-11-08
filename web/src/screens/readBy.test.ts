import { describe, it, expect } from "vitest";
import { readBy } from "./Detail";

const n = (name: string) => ({ name, role: "narrator", character: "", order: 0 }) as never;

describe("a book's narrators on its page (ADR 0116 step 3)", () => {
  it("reads as a sentence", () => {
    expect(readBy(undefined)).toBe("");
    expect(readBy([n("Kate Mulgrew")])).toBe("Read by Kate Mulgrew");
    expect(readBy([n("A"), n("B")])).toBe("Read by A and B");
    expect(readBy([n("A"), n("B"), n("C")])).toBe("Read by A, B and C");
    expect(readBy([n("A"), n("B"), n("C"), n("D"), n("E")])).toBe("Read by A, B, C and 2 more");
  });

  it("leaves out anybody credited for something else", () => {
    expect(readBy([{ name: "X", role: "actor", character: "", order: 0 } as never])).toBe("");
  });
});

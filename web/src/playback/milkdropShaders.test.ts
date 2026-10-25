/*
 * The check that a preset's own shaders compiled (ADR 0091, step g4).
 *
 * jsdom has no WebGL, so the context here is a stand-in that answers the five
 * questions shaderFaults asks. What the stand-in cannot prove is that the
 * vendored butterchurn still keeps its programs where shaderFaults looks, so
 * the last block reads that from the vendored source itself.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Preset, Visualizer } from "butterchurn";
import { shaderFaults, withDefaultShaders } from "./milkdropShaders";

const LINK_STATUS = 0x8b82;
const COMPILE_STATUS = 0x8b81;

interface FakeProgram {
  linked: boolean | null;
  shaders: { compiled: boolean; log: string }[];
  log: string;
}

function program(linked: boolean | null, shaderLog = "", programLog = ""): FakeProgram {
  return {
    linked,
    shaders: [
      { compiled: true, log: "" },
      { compiled: !shaderLog, log: shaderLog },
    ],
    log: programLog,
  };
}

function viz(warp: FakeProgram, comp: FakeProgram, lost = false): Visualizer {
  const gl = {
    LINK_STATUS,
    COMPILE_STATUS,
    isContextLost: () => lost,
    getProgramParameter: (p: FakeProgram, what: number) => (what === LINK_STATUS ? p.linked : null),
    getAttachedShaders: (p: FakeProgram) => p.shaders,
    getShaderParameter: (s: { compiled: boolean }, what: number) => (what === COMPILE_STATUS ? s.compiled : null),
    getShaderInfoLog: (s: { log: string }) => s.log,
    getProgramInfoLog: (p: FakeProgram) => p.log,
  };
  return {
    gl,
    renderer: { warpShader: { shaderProgram: warp }, compShader: { shaderProgram: comp } },
  } as unknown as Visualizer;
}

function preset(warp: string, comp: string): Preset {
  return {
    baseVals: { decay: 0.98 },
    init_eqs_eel: "",
    frame_eqs_eel: "",
    pixel_eqs_eel: "",
    warp,
    comp,
    shapes: [],
    waves: [],
  };
}

const own = preset("shader_body { ret = 1; }", "shader_body { ret = 0.5; }");

describe("shaderFaults", () => {
  it("is empty when both of the preset's programs linked", () => {
    expect(shaderFaults(viz(program(true), program(true)), own)).toEqual([]);
  });

  it("names the stage that failed, with the compile log's first line", () => {
    const comp = program(false, "ERROR: 0:141: 'xlat_mutablen' : undeclared identifier\nERROR: 1 compilation errors");
    expect(shaderFaults(viz(program(true), comp), own)).toEqual([
      { stage: "comp", log: "ERROR: 0:141: 'xlat_mutablen' : undeclared identifier" },
    ]);
  });

  it("names both when both failed", () => {
    const faults = shaderFaults(viz(program(false, "warp broke"), program(false, "comp broke")), own);
    expect(faults.map((f) => f.stage)).toEqual(["warp", "comp"]);
  });

  it("falls back to the program's log when every shader compiled but the link did not", () => {
    const warp = program(false, "", "Varyings over maximum\nmore");
    expect(shaderFaults(viz(warp, program(true)), own)).toEqual([{ stage: "warp", log: "Varyings over maximum" }]);
  });

  it("does not check a stage the preset leaves to butterchurn's default", () => {
    // Nothing better to fall back to, so a fault there is not the preset's.
    const noComp = preset("shader_body { ret = 1; }", "   ");
    expect(shaderFaults(viz(program(true), program(false, "x")), noComp)).toEqual([]);
  });

  it("does not blame a shader for a lost context", () => {
    expect(shaderFaults(viz(program(false, "x"), program(false, "y"), true), own)).toEqual([]);
    // A lost context answers null rather than false.
    expect(shaderFaults(viz(program(null), program(null)), own)).toEqual([]);
  });

  it("is empty, not a throw, for an engine without the fields it reads", () => {
    expect(shaderFaults({} as Visualizer, own)).toEqual([]);
  });
});

describe("withDefaultShaders", () => {
  it("empties only the stages named, and leaves the preset it was given alone", () => {
    const out = withDefaultShaders(own, ["comp"]);
    expect(out.comp).toBe("");
    expect(out.warp).toBe(own.warp);
    expect(out.baseVals).toEqual(own.baseVals);
    expect(own.comp).toBe("shader_body { ret = 0.5; }");
  });
});

describe("the vendored butterchurn still keeps what shaderFaults reads", () => {
  // vitest runs with web/ as the cwd (loadButterchurn.test.ts).
  const src = readFileSync(resolve(process.cwd(), "vendor/butterchurn/butterchurn.min.js"), "utf8");

  it("has its WebGL2 context on the visualizer as `gl`, and a `renderer` beside it", () => {
    expect(src).toContain('this.gl=this.internalCanvas.getContext("webgl2"');
    expect(src).toMatch(/this\.renderer=new \w+\(this\.gl,/);
  });

  it("has the warp and comp shaders on the renderer, each with its `shaderProgram`", () => {
    expect(src).toMatch(/this\.warpShader=new \w+\(/);
    expect(src).toMatch(/this\.compShader=new \w+\(/);
    // Both preset shaders build their program in createShader(t=""), which is
    // the form only they take: every other shader's createShader has no text.
    const preset = src.match(/createShader\(t=""\)\{[^]*?this\.shaderProgram=this\.gl\.createProgram\(\)/g) ?? [];
    expect(preset).toHaveLength(2);
  });

  it("compiles them inside the renderer's loadPreset, which shaderFaults is called straight after", () => {
    expect(src).toContain("this.warpShader.updateShader(o),this.compShader.updateShader(h)");
    // And the visualizer's loadPreset ends by handing over to it.
    expect(src).toMatch(/this\.renderer\.loadPreset\(t,e\)\}/);
  });
});

import type { Preset, Visualizer } from "butterchurn";

/*
 * Whether a preset's own shaders run here (ADR 0091, step g4).
 *
 * butterchurn compiles a preset's warp and comp shaders inside loadPreset and
 * never checks the result. A shader that fails to compile leaves an unlinked
 * program, and drawing with one draws nothing: the preset renders black, with
 * no error anywhere. The server converts shaders at upload (g3), and the
 * converter's GLSL compiles in 99.5% of the spike's sample, so this is the
 * guard for the rest: a texture the preset names but nobody uploaded, or a
 * name the converter left undeclared.
 *
 * What fails is replaced by butterchurn's default for that stage alone, so a
 * preset whose comp shader fails keeps its own warp. That is how MilkDrop
 * treats a shader it cannot compile.
 */

export type ShaderStage = "warp" | "comp";

export interface ShaderFault {
  stage: ShaderStage;
  /** The first line of the driver's log, or "" when it gave none. */
  log: string;
}

/*
 * butterchurn's internals, reached past the typings on purpose: the typings
 * (butterchurn.d.ts) are LANcast's contract with the library, and these two
 * fields are not part of it. milkdropShaders.test.ts pins that the vendored
 * build still has them, so an upgrade that renames one fails a test instead
 * of turning this check off without a word.
 */
interface Inside {
  gl?: WebGL2RenderingContext;
  renderer?: {
    warpShader?: { shaderProgram?: WebGLProgram | null };
    compShader?: { shaderProgram?: WebGLProgram | null };
  };
}

const STAGES: ShaderStage[] = ["warp", "comp"];

/*
 * shaderFaults: the stages of the preset just loaded whose program did not
 * link. Call it straight after loadPreset resolves, before a frame is drawn:
 * butterchurn keeps the outgoing preset's programs too, for the blend, and
 * swaps the two on every load.
 *
 * Only a stage the preset brings its own text for is checked. An empty stage
 * is butterchurn's default, which a fallback could not improve on.
 *
 * A lost context answers null rather than false. That is not the shader's
 * fault, so it is not reported as one.
 */
export function shaderFaults(viz: Visualizer, preset: Preset): ShaderFault[] {
  const { gl, renderer } = viz as unknown as Inside;
  if (!gl || !renderer || gl.isContextLost()) return [];
  const faults: ShaderFault[] = [];
  for (const stage of STAGES) {
    if (!preset[stage]?.trim()) continue;
    const program = renderer[stage === "warp" ? "warpShader" : "compShader"]?.shaderProgram;
    if (!program) continue;
    if (gl.getProgramParameter(program, gl.LINK_STATUS) !== false) continue;
    faults.push({ stage, log: faultLog(gl, program) });
  }
  return faults;
}

/*
 * The first line that says why: a shader's compile log when one of them
 * failed (the program log then only repeats that it could not link), the
 * program's own log otherwise.
 */
function faultLog(gl: WebGL2RenderingContext, program: WebGLProgram): string {
  for (const shader of gl.getAttachedShaders(program) ?? []) {
    if (gl.getShaderParameter(shader, gl.COMPILE_STATUS) === false) {
      return firstLine(gl.getShaderInfoLog(shader));
    }
  }
  return firstLine(gl.getProgramInfoLog(program));
}

function firstLine(log: string | null): string {
  return (log ?? "").trim().split(/\r?\n/, 1)[0] ?? "";
}

/** The preset with `stages` emptied, which butterchurn plays with its defaults. */
export function withDefaultShaders(preset: Preset, stages: readonly ShaderStage[]): Preset {
  const out = { ...preset };
  for (const stage of stages) out[stage] = "";
  return out;
}

import { mixRGB, thickStrip, wavePoints, type FrameParams } from "./trailMath";
import type { RGB } from "./vizMath";

/*
 * The feedback trails' renderer (docs/now-playing-plan.md, "Tier 2 — feedback
 * trails"): AVS and MilkDrop's core trick, in WebGL2 and nothing else.
 *
 * Two framebuffers, swapped each frame. The transform pass draws the previous
 * frame into the other buffer slightly zoomed, turned, drifted, blurred and
 * faded; the waveform pass draws this frame's line on top; the result is
 * blitted to the canvas, and next frame it is the one transformed. Every
 * number either pass uses comes from trailMath.ts's frameParams, so this file
 * holds GL plumbing and no decisions.
 *
 * Colour is premultiplied all the way through and the buffers start
 * transparent, so the trails fade into the cover's wash behind the canvas
 * rather than into a black square: fading multiplies alpha with colour.
 */

let supported: boolean | undefined;

/*
 * webgl2Supported, asked once. jsdom and old WebViews have no
 * WebGL2RenderingContext at all, which is checked before a canvas is made so
 * a test run does not log "not implemented" for every render.
 */
export function webgl2Supported(): boolean {
  if (supported !== undefined) return supported;
  supported = false;
  try {
    if (typeof WebGL2RenderingContext === "undefined" || typeof document === "undefined") return supported;
    const gl = document.createElement("canvas").getContext("webgl2");
    supported = !!gl;
    gl?.getExtension("WEBGL_lose_context")?.loseContext();
  } catch {
    supported = false;
  }
  return supported;
}

/** For tests: say what the browser supports. `undefined` asks it again. */
export function setWebGL2ForTests(v: boolean | undefined) {
  supported = v;
}

const QUAD_VS = `#version 300 es
in vec2 pos;
out vec2 uv;
void main() {
  uv = pos * 0.5 + 0.5;
  gl_Position = vec4(pos, 0.0, 1.0);
}`;

/*
 * The transform pass. Sampling at the inverse of the motion is what makes the
 * picture move: to zoom out, each pixel reads from nearer the middle. Outside
 * the previous frame is transparent, so nothing smears in from the edges.
 * Five taps, a cross, for the blur: cheap, and in a field that is redrawn
 * sixty times a second, enough.
 */
const TRANSFORM_FS = `#version 300 es
precision mediump float;
in vec2 uv;
out vec4 color;
uniform sampler2D prev;
uniform vec2 texel;
uniform float zoom;
uniform float angle;
uniform float aspect;
uniform float drift;
uniform float fade;
uniform float blur;
vec4 tap(vec2 p) {
  if (p.x < 0.0 || p.y < 0.0 || p.x > 1.0 || p.y > 1.0) return vec4(0.0);
  return texture(prev, p);
}
void main() {
  vec2 d = uv - 0.5;
  d.x *= aspect;
  float c = cos(-angle);
  float s = sin(-angle);
  d = mat2(c, s, -s, c) * d / zoom;
  d.x /= aspect;
  vec2 p = d + 0.5 - vec2(0.0, drift);
  vec2 o = texel * blur;
  vec4 sum = tap(p) * 0.4
    + tap(p + vec2(o.x, 0.0)) * 0.15 + tap(p - vec2(o.x, 0.0)) * 0.15
    + tap(p + vec2(0.0, o.y)) * 0.15 + tap(p - vec2(0.0, o.y)) * 0.15;
  color = sum * fade;
}`;

const WAVE_VS = `#version 300 es
in vec2 pos;
void main() {
  gl_Position = vec4(pos, 0.0, 1.0);
}`;

const WAVE_FS = `#version 300 es
precision mediump float;
out vec4 color;
uniform vec4 tint;
void main() {
  color = tint;
}`;

/** Points in the waveform: enough to read as a line, few enough to stay cheap. */
const WAVE_POINTS = 256;

export interface TrailRenderer {
  /** Draw one frame. `td` is the analyser's time-domain bytes. */
  frame(td: Uint8Array, p: FrameParams, palette: RGB[]): void;
  /** Size the buffers; a change clears the trails. */
  resize(w: number, h: number): void;
  dispose(): void;
}

function compile(gl: WebGL2RenderingContext, vs: string, fs: string): WebGLProgram | null {
  const make = (type: number, src: string) => {
    const sh = gl.createShader(type);
    if (!sh) return null;
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
      gl.deleteShader(sh);
      return null;
    }
    return sh;
  };
  const v = make(gl.VERTEX_SHADER, vs);
  const f = make(gl.FRAGMENT_SHADER, fs);
  if (!v || !f) return null;
  const prog = gl.createProgram();
  if (!prog) return null;
  gl.attachShader(prog, v);
  gl.attachShader(prog, f);
  gl.bindAttribLocation(prog, 0, "pos");
  gl.linkProgram(prog);
  gl.deleteShader(v);
  gl.deleteShader(f);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    gl.deleteProgram(prog);
    return null;
  }
  return prog;
}

/*
 * createTrailRenderer, or null where the canvas will not give a WebGL2
 * context or a shader will not compile. The caller draws nothing then; the
 * stage switcher only offers trails where webgl2Supported() said yes, so null
 * here is a driver refusing, not a normal path.
 */
export function createTrailRenderer(canvas: HTMLCanvasElement): TrailRenderer | null {
  const gl = canvas.getContext("webgl2", {
    alpha: true,
    premultipliedAlpha: true,
    // Blitting into a multisampled canvas is an error, and a trail has no
    // edges to antialias.
    antialias: false,
    depth: false,
    stencil: false,
    preserveDrawingBuffer: false,
  });
  if (!gl) return null;
  const transform = compile(gl, QUAD_VS, TRANSFORM_FS);
  const wave = compile(gl, WAVE_VS, WAVE_FS);
  if (!transform || !wave) return null;

  const u = (prog: WebGLProgram, name: string) => gl.getUniformLocation(prog, name);
  const ut = {
    prev: u(transform, "prev"),
    texel: u(transform, "texel"),
    zoom: u(transform, "zoom"),
    angle: u(transform, "angle"),
    aspect: u(transform, "aspect"),
    drift: u(transform, "drift"),
    fade: u(transform, "fade"),
    blur: u(transform, "blur"),
  };
  const tint = u(wave, "tint");

  const quad = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, quad);
  // One triangle that covers the field, rather than two that meet down the middle.
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 3, -1, -1, 3]), gl.STATIC_DRAW);
  const strip = gl.createBuffer();
  const pts = new Float32Array(WAVE_POINTS * 2);
  const verts = new Float32Array(WAVE_POINTS * 4);

  let w = 0;
  let h = 0;
  let tex: (WebGLTexture | null)[] = [];
  let fbo: (WebGLFramebuffer | null)[] = [];
  let src = 0;
  let lost = false;

  const onLost = (e: Event) => {
    // Stop drawing; the next mount builds a new renderer.
    e.preventDefault();
    lost = true;
  };
  canvas.addEventListener("webglcontextlost", onLost);

  const freeBuffers = () => {
    for (const t of tex) gl.deleteTexture(t);
    for (const f of fbo) gl.deleteFramebuffer(f);
    tex = [];
    fbo = [];
  };

  const resize = (nw: number, nh: number) => {
    if (lost || (nw === w && nh === h && tex.length)) return;
    w = nw;
    h = nh;
    canvas.width = w;
    canvas.height = h;
    freeBuffers();
    for (let i = 0; i < 2; i++) {
      const t = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, t);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA8, w, h, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
      const f = gl.createFramebuffer();
      gl.bindFramebuffer(gl.FRAMEBUFFER, f);
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, t, 0);
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT);
      tex.push(t);
      fbo.push(f);
    }
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    src = 0;
  };

  const frame = (td: Uint8Array, p: FrameParams, palette: RGB[]) => {
    if (lost || !tex.length) return;
    const dst = 1 - src;
    const aspect = w / Math.max(1, h);
    gl.viewport(0, 0, w, h);

    // The previous frame, moved.
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo[dst]);
    gl.disable(gl.BLEND);
    gl.useProgram(transform);
    gl.activeTexture(gl.TEXTURE0);
    gl.bindTexture(gl.TEXTURE_2D, tex[src]);
    gl.uniform1i(ut.prev, 0);
    gl.uniform2f(ut.texel, 1 / w, 1 / h);
    gl.uniform1f(ut.zoom, p.zoom);
    gl.uniform1f(ut.angle, p.rotate);
    gl.uniform1f(ut.aspect, aspect);
    gl.uniform1f(ut.drift, p.drift);
    gl.uniform1f(ut.fade, p.fade);
    gl.uniform1f(ut.blur, p.blur);
    gl.bindBuffer(gl.ARRAY_BUFFER, quad);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    gl.drawArrays(gl.TRIANGLES, 0, 3);

    // This frame's line, on top.
    wavePoints(td, WAVE_POINTS, p.ring, p.amp, aspect, pts);
    // About three buffer pixels thick, whatever the buffer's size.
    thickStrip(pts, 6 / Math.max(1, h), 1 / aspect, 1, verts);
    gl.useProgram(wave);
    const [r, g, b] = mixRGB(palette[0], palette[1] ?? palette[0], p.mix);
    const a = p.bright;
    gl.uniform4f(tint, r * a, g * a, b * a, a);
    gl.enable(gl.BLEND);
    gl.blendFunc(gl.ONE, gl.ONE_MINUS_SRC_ALPHA);
    gl.bindBuffer(gl.ARRAY_BUFFER, strip);
    gl.bufferData(gl.ARRAY_BUFFER, verts, gl.DYNAMIC_DRAW);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, WAVE_POINTS * 2);

    // To the canvas.
    gl.bindFramebuffer(gl.READ_FRAMEBUFFER, fbo[dst]);
    gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, null);
    gl.blitFramebuffer(0, 0, w, h, 0, 0, w, h, gl.COLOR_BUFFER_BIT, gl.NEAREST);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    src = dst;
  };

  return {
    frame,
    resize,
    dispose() {
      canvas.removeEventListener("webglcontextlost", onLost);
      if (lost) return;
      freeBuffers();
      gl.deleteBuffer(quad);
      gl.deleteBuffer(strip);
      gl.deleteProgram(transform);
      gl.deleteProgram(wave);
    },
  };
}

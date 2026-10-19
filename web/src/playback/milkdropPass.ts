import { GUARD, LIMIT_GRID, LIMIT_MAX_DT } from "./limiterMath";

/*
 * The output pass every MilkDrop preset is drawn through (ADR 0091,
 * Decision 7): LANcast's own WebGL2 context on the visible canvas, taking
 * butterchurn's finished frame as a texture. Whatever a preset does, what
 * reaches the screen has been through this.
 *
 * Three passes, every number from limiterMath.ts:
 *  1. guard: each pixel's hue moved out of the gold band, exactly as
 *     vizMath's awayFromGold does, into an sRGB texture. Storing it as sRGB
 *     makes the GPU's own mipmaps averages of *linear* light, which is what
 *     WCAG's relative luminance is measured in.
 *  2. limit: a LIMIT_GRID² grid. Each cell reads its mean luminance from the
 *     mipmaps, moves its remembered value towards it by at most rate × dt, and
 *     stores both that value and the correction that lands the cell on it.
 *     Two textures, swapped each frame, so the memory is the GPU's.
 *  3. output: each pixel takes the correction of the cells around it,
 *     interpolated so no cell edge shows, and is written to the canvas.
 *
 * The grid is RGBA8, which every WebGL2 device can render to (float targets
 * need an extension a TV may lack), with each value packed into two bytes:
 * 1/65 025 of precision, far below what a frame's allowance moves.
 *
 * The correction is interpolated between cells, so a window's mean is held
 * exactly only where neighbouring cells agree. A full-field flash, the case
 * WCAG 2.3.1 is about, is every cell at once, and there they all agree.
 */

const QUAD_VS = `#version 300 es
in vec2 pos;
out vec2 uv;
void main() {
  uv = pos * 0.5 + 0.5;
  gl_Position = vec4(pos, 0.0, 1.0);
}`;

const f = (n: number) => n.toFixed(4);

/*
 * The guard, transcribed from limiterMath.guardHue and vizMath's HSL
 * conversions. The band's numbers are written into the source from GUARD, so
 * the two cannot disagree on one.
 */
const GUARD_FS = `#version 300 es
precision highp float;
in vec2 uv;
out vec4 color;
uniform sampler2D src;

vec3 rgb2hsl(vec3 c) {
  float mx = max(c.r, max(c.g, c.b));
  float mn = min(c.r, min(c.g, c.b));
  float l = (mx + mn) * 0.5;
  if (mx == mn) return vec3(0.0, 0.0, l);
  float d = mx - mn;
  float s = l > 0.5 ? d / (2.0 - mx - mn) : d / (mx + mn);
  float h;
  if (mx == c.r) h = (c.g - c.b) / d + (c.g < c.b ? 6.0 : 0.0);
  else if (mx == c.g) h = (c.b - c.r) / d + 2.0;
  else h = (c.r - c.g) / d + 4.0;
  return vec3(h * 60.0, s, l);
}

float hue2rgb(float p, float q, float t) {
  if (t < 0.0) t += 1.0;
  if (t > 1.0) t -= 1.0;
  if (t < 1.0 / 6.0) return p + (q - p) * 6.0 * t;
  if (t < 0.5) return q;
  if (t < 2.0 / 3.0) return p + (q - p) * (2.0 / 3.0 - t) * 6.0;
  return p;
}

vec3 hsl2rgb(vec3 hsl) {
  float h = mod(hsl.x, 360.0) / 360.0;
  float s = hsl.y;
  float l = hsl.z;
  if (s == 0.0) return vec3(l);
  float q = l < 0.5 ? l * (1.0 + s) : l + s - l * s;
  float p = 2.0 * l - q;
  return vec3(hue2rgb(p, q, h + 1.0 / 3.0), hue2rgb(p, q, h), hue2rgb(p, q, h - 1.0 / 3.0));
}

vec3 toLinear(vec3 c) {
  return mix(c / 12.92, pow((c + 0.055) / 1.055, vec3(2.4)), step(vec3(0.04045), c));
}

void main() {
  vec3 c = texture(src, uv).rgb;
  vec3 hsl = rgb2hsl(c);
  if (hsl.y >= ${f(GUARD.minSat)} && hsl.x >= ${f(GUARD.min)} && hsl.x <= ${f(GUARD.max)}) {
    hsl.x = hsl.x < ${f(GUARD.mid)} ? ${f(GUARD.below)} : ${f(GUARD.above)};
    c = hsl2rgb(hsl);
  }
  // The target is sRGB, so the GPU encodes this on the way in and decodes it
  // on every read: everything after here is linear light.
  color = vec4(toLinear(c), 1.0);
}`;

const PACK = `
vec2 pack(float x) {
  x = clamp(x, 0.0, 1.0) * 255.0;
  return vec2(floor(x) / 255.0, fract(x));
}
float unpack(vec2 p) {
  return p.x + p.y / 255.0;
}
float lum(vec3 c) {
  return dot(c, vec3(0.2126, 0.7152, 0.0722));
}`;

/*
 * The limit pass, one fragment per cell. limiterMath.slewLuma and
 * limiterMath.correction, transcribed. The cell's mean is sixteen taps of a
 * mip level a quarter of the cell across, so the whole cell is read without
 * reading every pixel of it.
 */
const LIMIT_FS = `#version 300 es
precision highp float;
out vec4 color;
uniform sampler2D guarded;
uniform sampler2D prev;
uniform float lod;
uniform float allow;
uniform bool first;
${PACK}
void main() {
  ivec2 cell = ivec2(gl_FragCoord.xy);
  vec2 base = vec2(cell) / ${f(LIMIT_GRID)};
  float sum = 0.0;
  for (int y = 0; y < 4; y++) {
    for (int x = 0; x < 4; x++) {
      vec2 at = base + (vec2(float(x), float(y)) + 0.5) / (4.0 * ${f(LIMIT_GRID)});
      sum += lum(textureLod(guarded, at, lod).rgb);
    }
  }
  float src = clamp(sum / 16.0, 0.0, 1.0);
  float lim = first ? src : unpack(texelFetch(prev, cell, 0).rg);
  lim = clamp(src, lim - allow, lim + allow);
  float v = 0.0;
  if (abs(lim - src) >= 1e-6) {
    if (lim < src) v = src > 0.0 ? lim / src - 1.0 : 0.0;
    else v = src < 1.0 ? min(1.0, (lim - src) / (1.0 - src)) : 0.0;
  }
  color = vec4(pack(lim), pack(v * 0.5 + 0.5));
}`;

/*
 * The output pass. The four cells nearest the pixel, blended by hand: the
 * values are packed, and a GPU's own filtering would blend the bytes, not the
 * numbers.
 */
const OUTPUT_FS = `#version 300 es
precision highp float;
in vec2 uv;
out vec4 color;
uniform sampler2D guarded;
uniform sampler2D state;
${PACK}
float corr(ivec2 c) {
  c = clamp(c, ivec2(0), ivec2(${LIMIT_GRID - 1}));
  return unpack(texelFetch(state, c, 0).ba) * 2.0 - 1.0;
}
vec3 toSRGB(vec3 c) {
  c = clamp(c, 0.0, 1.0);
  return mix(c * 12.92, 1.055 * pow(c, vec3(1.0 / 2.4)) - 0.055, step(vec3(0.0031308), c));
}
void main() {
  vec3 c = textureLod(guarded, uv, 0.0).rgb;
  vec2 p = uv * ${f(LIMIT_GRID)} - 0.5;
  ivec2 i = ivec2(floor(p));
  vec2 t = fract(p);
  float v = mix(
    mix(corr(i), corr(i + ivec2(1, 0)), t.x),
    mix(corr(i + ivec2(0, 1)), corr(i + ivec2(1, 1)), t.x),
    t.y);
  c = v < 0.0 ? c * (1.0 + v) : mix(c, vec3(1.0), v);
  color = vec4(toSRGB(c), 1.0);
}`;

export interface OutputPass {
  /**
   * Draw `src` (butterchurn's finished frame) to the canvas through the guard
   * and the limiter. `rate` is the limiter's allowance per second.
   */
  frame(src: TexImageSource, w: number, h: number, dt: number, rate: number): void;
  /** Forget the limiter's memory: the next frame is taken as drawn. */
  reset(): void;
  dispose(): void;
}

function compile(gl: WebGL2RenderingContext, fs: string): WebGLProgram | null {
  const make = (type: number, src: string) => {
    const sh = gl.createShader(type);
    if (!sh) return null;
    gl.shaderSource(sh, src);
    gl.compileShader(sh);
    if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
      console.warn("milkdrop pass: shader did not compile", gl.getShaderInfoLog(sh));
      gl.deleteShader(sh);
      return null;
    }
    return sh;
  };
  const v = make(gl.VERTEX_SHADER, QUAD_VS);
  const fr = make(gl.FRAGMENT_SHADER, fs);
  if (!v || !fr) return null;
  const prog = gl.createProgram();
  if (!prog) return null;
  gl.attachShader(prog, v);
  gl.attachShader(prog, fr);
  gl.bindAttribLocation(prog, 0, "pos");
  gl.linkProgram(prog);
  gl.deleteShader(v);
  gl.deleteShader(fr);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    gl.deleteProgram(prog);
    return null;
  }
  return prog;
}

/*
 * createOutputPass, or null where the canvas will not give WebGL2 or a shader
 * will not compile. Null means MilkDrop is not drawn at all: there is no
 * unguarded fallback, because a preset without this pass is exactly what the
 * ADR refuses to show.
 */
export function createOutputPass(canvas: HTMLCanvasElement): OutputPass | null {
  const gl = canvas.getContext("webgl2", { alpha: false, antialias: false, depth: false, stencil: false });
  // A lost context compiles nothing, and says so only as a null info log.
  if (!gl || gl.isContextLost()) return null;
  const guardProg = compile(gl, GUARD_FS);
  const limitProg = compile(gl, LIMIT_FS);
  const outProg = compile(gl, OUTPUT_FS);
  if (!guardProg || !limitProg || !outProg) return null;

  const quad = gl.createBuffer();
  gl.bindBuffer(gl.ARRAY_BUFFER, quad);
  gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1, -1, 1, -1, -1, 1, 1, 1]), gl.STATIC_DRAW);
  const vao = gl.createVertexArray();
  gl.bindVertexArray(vao);
  gl.enableVertexAttribArray(0);
  gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 0, 0);

  const srcTex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, srcTex);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

  // The grid: two RGBA8 textures, each with its own framebuffer.
  const grid = [0, 1].map(() => {
    const tex = gl.createTexture()!;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texStorage2D(gl.TEXTURE_2D, 1, gl.RGBA8, LIMIT_GRID, LIMIT_GRID);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    const fb = gl.createFramebuffer()!;
    gl.bindFramebuffer(gl.FRAMEBUFFER, fb);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    return { tex, fb };
  });
  gl.bindFramebuffer(gl.FRAMEBUFFER, null);

  let guarded: { tex: WebGLTexture; fb: WebGLFramebuffer; w: number; h: number; levels: number } | null = null;
  let front = 0;
  let fresh = true;

  const sizeGuarded = (w: number, h: number) => {
    if (guarded && guarded.w === w && guarded.h === h) return guarded;
    if (guarded) {
      gl.deleteTexture(guarded.tex);
      gl.deleteFramebuffer(guarded.fb);
    }
    const levels = Math.floor(Math.log2(Math.max(w, h))) + 1;
    const tex = gl.createTexture()!;
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texStorage2D(gl.TEXTURE_2D, levels, gl.SRGB8_ALPHA8, w, h);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    const fb = gl.createFramebuffer()!;
    gl.bindFramebuffer(gl.FRAMEBUFFER, fb);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    guarded = { tex, fb, w, h, levels };
    return guarded;
  };

  const u = (prog: WebGLProgram, name: string) => gl.getUniformLocation(prog, name);
  const U = {
    guardSrc: u(guardProg, "src"),
    limGuarded: u(limitProg, "guarded"),
    limPrev: u(limitProg, "prev"),
    limLod: u(limitProg, "lod"),
    limStep: u(limitProg, "allow"),
    limInit: u(limitProg, "first"),
    outGuarded: u(outProg, "guarded"),
    outState: u(outProg, "state"),
  };

  return {
    frame(src, w, h, dt, rate) {
      if (gl.isContextLost()) return;
      if (canvas.width !== w || canvas.height !== h) {
        canvas.width = w;
        canvas.height = h;
      }
      const g = sizeGuarded(w, h);
      gl.disable(gl.BLEND);
      gl.bindVertexArray(vao);

      // Butterchurn's frame in, top row first, as a canvas is.
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, srcTex);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, true);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, src);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);

      // 1. The guard, into the sRGB texture, and its mipmaps.
      gl.bindFramebuffer(gl.FRAMEBUFFER, g.fb);
      gl.viewport(0, 0, w, h);
      gl.useProgram(guardProg);
      gl.uniform1i(U.guardSrc, 0);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      gl.bindTexture(gl.TEXTURE_2D, g.tex);
      gl.generateMipmap(gl.TEXTURE_2D);

      // 2. The limiter's grid, from the front buffer into the back one.
      const back = 1 - front;
      gl.bindFramebuffer(gl.FRAMEBUFFER, grid[back].fb);
      gl.viewport(0, 0, LIMIT_GRID, LIMIT_GRID);
      gl.useProgram(limitProg);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, g.tex);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, grid[front].tex);
      gl.uniform1i(U.limGuarded, 0);
      gl.uniform1i(U.limPrev, 1);
      const cell = Math.min(w, h) / LIMIT_GRID;
      gl.uniform1f(U.limLod, Math.max(0, Math.min(g.levels - 1, Math.log2(cell / 4))));
      gl.uniform1f(U.limStep, rate * Math.min(LIMIT_MAX_DT, Math.max(0, dt)));
      gl.uniform1i(U.limInit, fresh ? 1 : 0);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      front = back;
      fresh = false;

      // 3. Out to the canvas.
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.viewport(0, 0, w, h);
      gl.useProgram(outProg);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, g.tex);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, grid[front].tex);
      gl.uniform1i(U.outGuarded, 0);
      gl.uniform1i(U.outState, 1);
      gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
      gl.activeTexture(gl.TEXTURE0);
    },
    reset() {
      fresh = true;
    },
    dispose() {
      for (const p of [guardProg, limitProg, outProg]) gl.deleteProgram(p);
      for (const c of grid) {
        gl.deleteTexture(c.tex);
        gl.deleteFramebuffer(c.fb);
      }
      if (guarded) {
        gl.deleteTexture(guarded.tex);
        gl.deleteFramebuffer(guarded.fb);
      }
      gl.deleteTexture(srcTex);
      gl.deleteBuffer(quad);
      gl.deleteVertexArray(vao);
      /*
       * No loseContext() here. A canvas hands out one context for its whole
       * life, so losing it bricks the element: the next pass made on the same
       * canvas compiles nothing. React's StrictMode does exactly that in
       * development (unmount and remount, same DOM), and it turned the stage
       * black on the first return to Now Playing. The context goes when the
       * canvas is collected.
       */
    },
  };
}

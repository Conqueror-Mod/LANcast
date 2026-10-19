// Types for the vendored butterchurn build (butterchurn.min.js beside this
// file). Upstream ships no declarations, so these are written by hand from the
// source at the pinned commit (src/index.js, src/visualizer.js), and they cover
// only the surface LANcast is expected to use. Anything not declared here is
// not part of LANcast's contract with the library, whether or not it exists.
//
// `loadJSPreset` is deliberately absent: the LANcast build has no such method
// (no-js-presets.patch), and these types would not admit it if it did.

export interface VisualizerOptions {
  width?: number;
  height?: number;
  pixelRatio?: number;
  textureRatio?: number;
  meshWidth?: number;
  meshHeight?: number;
  outputFXAA?: boolean;
  /**
   * Upstream's switch that keeps a preset off the `new Function` path. The
   * LANcast build has no such path left, and `loadButterchurn` sets this to
   * true regardless, so it is never a decision a caller makes.
   */
  onlyUseWASM?: boolean;
}

export interface RenderOptions {
  audioLevels?: {
    timeByteArray: Uint8Array;
    timeByteArrayL: Uint8Array;
    timeByteArrayR: Uint8Array;
  };
  elapsedTime?: number;
}

/**
 * A preset in butterchurn's own v3 JSON shape. Only the EEL form is accepted
 * by this build: a preset without `init_eqs_eel` is refused with an error.
 */
export interface Preset {
  version?: number;
  baseVals: Record<string, number>;
  init_eqs_eel: string;
  frame_eqs_eel: string;
  pixel_eqs_eel: string;
  warp?: string;
  comp?: string;
  shapes: Array<{
    baseVals: Record<string, number>;
    init_eqs_eel: string;
    frame_eqs_eel: string;
  }>;
  waves: Array<{
    baseVals: Record<string, number>;
    init_eqs_eel: string;
    frame_eqs_eel: string;
    point_eqs_eel: string;
  }>;
}

export interface Visualizer {
  connectAudio(node: AudioNode): void;
  disconnectAudio(node: AudioNode): void;
  /** Rejects if the preset has no EEL, or if eel-wasm cannot compile it. */
  loadPreset(preset: Preset, blendTime?: number): Promise<void>;
  setRendererSize(
    width: number,
    height: number,
    opts?: { pixelRatio?: number; textureRatio?: number; meshWidth?: number; meshHeight?: number },
  ): void;
  setInternalMeshSize(width: number, height: number): void;
  setOutputAA(useAA: boolean): void;
  setCanvas(canvas: HTMLCanvasElement): void;
  /** Can throw a WebAssembly.RuntimeError from inside a preset's equations. */
  render(opts?: RenderOptions): unknown;
}

declare const Butterchurn: {
  createVisualizer(
    context: AudioContext,
    canvas: HTMLCanvasElement,
    opts: VisualizerOptions,
  ): Visualizer;
};

export default Butterchurn;

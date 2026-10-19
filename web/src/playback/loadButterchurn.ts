import type {
  Visualizer as ButterchurnVisualizer,
  VisualizerOptions,
} from "butterchurn";

/*
 * The one door to butterchurn, the MilkDrop engine (ADR 0091).
 *
 * Nothing calls this yet. It exists so that the vendoring step can prove the
 * alias, the chunk split and the option below before any screen depends on
 * them; the MilkDrop tier in Now Playing is a later step.
 *
 * `import("butterchurn")` can only mean web/vendor/butterchurn/butterchurn.min.js:
 * the name is aliased in vite.config.ts and vitest.config.ts, and butterchurn
 * is not in package.json, so there is no registry copy for it to mean instead.
 * Being a dynamic import, the library is its own chunk and costs nothing until
 * somebody picks a MilkDrop preset.
 *
 * Importing the chunk has side effects on the page, recorded in the vendor
 * README: it adds a few properties to the global `Math`, and it assigns about
 * twenty small functions (`sqr`, `rand`, `bor`…) onto `window`. Nothing in
 * LANcast may rely on either.
 */

export type MilkdropOptions = Omit<VisualizerOptions, "onlyUseWASM">;

export type MilkdropEngine = {
  createVisualizer(
    context: AudioContext,
    canvas: HTMLCanvasElement,
    opts?: MilkdropOptions,
  ): ButterchurnVisualizer;
};

export async function loadButterchurn(): Promise<MilkdropEngine> {
  const mod = await import("butterchurn");
  const Butterchurn = mod.default;
  return {
    createVisualizer(context, canvas, opts = {}) {
      /*
       * onlyUseWASM is set last, so no caller's options can turn it off.
       *
       * The vendored build has had its JavaScript preset path removed, so this
       * is a second lock on a door that is already gone. It stays because the
       * option is what upstream checks, and an upgrade that forgot to re-apply
       * the patch would otherwise reopen `new Function` silently.
       */
      return Butterchurn.createVisualizer(context, canvas, {
        ...(opts as VisualizerOptions),
        onlyUseWASM: true,
      });
    },
  };
}

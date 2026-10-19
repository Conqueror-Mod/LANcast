# Step f′ — how MilkDrop groups EEL operators (ADR 0091)

**Evidence, not product code**, like the rest of `spikes/milkdrop`. It settles
the question ADR 0091 left open: does eel-wasm group `&` and `^` the way
Winamp's MilkDrop does? It does not. The outcome is
`web/vendor/butterchurn/eel-wasm-precedence.patch`. The decision is in
ADR 0091's amendment of 2026-10-18 and the vendor README's "The second
patch".

## The reference: MilkDrop 2's own front end

`md2ref.c` + `build.sh` compile MilkDrop 2's expression front end with gcc.
`build.sh` takes the path to `code/ns-eel2` in the open MilkDrop 2 source
(read here from the MilkDrop3 repository at `6b39088`). These files are
compiled **unchanged**:

| file | what it is |
|---|---|
| `nseel-caltab.c` | the bison tables and driver generated from MilkDrop's grammar, `cal.y` |
| `nseel-yylex.c`, `nseel-lextab.c` | the lexer |
| `nseel-eval.c` | identifier and function lookup, the variable table |
| `nseel-compiler.c` lines 1010–1455 | `preprocessCode()`, which rewrites `^ % ! == < > && \|\|` and friends as calls before parsing |

`md2ref.c` supplies only what `nseel-compiler.c` would:
- the function table (names and arities, copied in order);
- `nseel_createCompiled*`, which here build a tree instead of emitting x86;
- the statement split from `NSEEL_code_compile`.

It prints the tree and evaluates it. `&` and `|` truncate both sides to int64,
as the x87 asm does under the `_RC_CHOP` mode that `GLUE_CALL_CODE` sets.
Nothing of MilkDrop's runs as shipped, and no binary was downloaded.

Build it with K&R-era C (`-std=gnu89`): the lexer tables are declared that
way.

**How far to trust it.** `testcases-md2.mjs` runs eel-wasm's own
`tools/testCases.ts` through it. Its author confirmed those cases in real
MilkDrop by turning them into a preset (`buildTestPreset.js`). Of the 124 it
can evaluate, it reproduces **122**. The two misses are not about grouping:
- a `\\` comment, which MilkDrop's preset loader strips
  (`CState::StripLinefeedCharsAndComments`) and ns-eel2 never sees;
- division by zero, a runtime detail.

The 16 cases this step added get their expected values from md2ref, so they
pass by construction. They still need a run in real Winamp (below).

MilkDrop 1 used AVS's evallib. Its `cal.y` is in the AVS open-source release
(`avs/vis_avs/evallib/cal.y`, lines 22–27). The `|`, `&`, `+ -`, `* /` and
unary-minus declarations there are the ones ns-eel2's tables encode.

## The corpus

`canon.mjs` reads the block dump that `internal/milkdrop/corpus_test.go` writes
(`LANCAST_MILKDROP_DIFF`). For each block it then:
1. strips comments and joins lines as MilkDrop's loader does;
2. parses the result with md2ref and with eel-wasm's own `parse()`, taken
   from the vendored bundle;
3. maps both trees onto one canonical form;
4. evaluates both with **one** evaluator, using the same seeded variables over
   three input ranges.

So a value difference can only be a difference of grouping. `causes.mjs`
splits the presets the patch fixed by operator.

| | before (eel-wasm 0.0.16) | after (patched) |
|---|---|---|
| blocks compared | 41,278 non-empty | same |
| presets whose parse differs | 2,733 | 18 |
| presets whose **value** differs | **124** | 14 |
| …of which grouping | 110: `&` 105, `!` 4, `&&` 1 | 0 |

The 14 that remain are not grouping:
- integer literals above 2³¹, which MilkDrop's `atoi` saturates (11);
- `_aboeq()` called by name (3), which the Go validator refuses anyway.

`results/` holds the summaries, with examples and no local paths.

## Running it

```sh
sh build.sh <MilkDrop3>/code/ns-eel2                 # -> out/md2ref.exe
# a copy of the vendored bundle with eel-wasm's internals exported; the
# minified names are this build's (find them with the patterns in the README
# of web/vendor/butterchurn if the artefact changes):
cp ../../../web/vendor/butterchurn/butterchurn.min.js bc.mjs
printf '\nexport{it as __compileModule,H as __shims,m as __parse};\n' >> bc.mjs
node probe-table.mjs out/md2ref.exe <stock bc.mjs> bc.mjs ../eel.mjs probes.txt
node testcases-md2.mjs out/md2ref.exe <eel-wasm>/packages/compiler/tools/testCases.ts
node canon.mjs out/md2ref.exe bc.mjs <blocks.jsonl> <report.json>
node causes.mjs <report before> <report after>
```

The `bc.mjs` copies are scratch files; do not commit them. "Stock" means the
artefact before the patch (`git show 054cddba:web/vendor/butterchurn/butterchurn.min.js`).

## Owed

- **Load `results/precedence-test.milk` in real Winamp's MilkDrop 2.**
  - `test-preset.mjs` generated it in `buildTestPreset.js`'s exact form, from
    the patched `testCases.ts`. That script can't run as shipped: it
    `require()`s an ES-module file.
  - It holds the 23 precedence cases: the author's 7 and the 16 new ones.
  - A green screen means MilkDrop agrees with all 23. A red screen means one
    failed, and `monitor` (press N) gives its number in `test-preset.mjs`'s
    list.
  - Already checked here:
    - md2ref passes it (`q1=1`) and the patched build passes it.
    - Stock eel-wasm fails it (`q1=0`), and even leaves `monitor` at −1. The
      preset's own line `if(!pass && monitor == -1, …)` assumes MilkDrop's
      grouping of `&&`, which stock eel-wasm does not share.
- File the upstream issue: `UPSTREAM.md`.

# Draft for upstream — NOT POSTED

For Chris to file, or not, on captbaritone/eel-wasm. Nothing below has been
posted anywhere. The patch is `web/vendor/butterchurn/eel-wasm-precedence.patch`
(against `f7394d3`, the 0.0.16 release commit). It applies cleanly with
`git apply`.

Once eel-wasm releases the fix, butterchurn takes it with a lockfile bump. A
one-line issue there pointing at the release is enough.

---

## Issue title

Operator precedence differs from MilkDrop for `&`, `|`, `!`, `&&`/`||` and unary minus

## Issue body

eel-wasm groups several EEL operators differently from MilkDrop 2's ns-eel2.
As a result, some presets compute different values than they did in Winamp.

**What MilkDrop does.** ns-eel2 (2004–2008, as in the open MilkDrop 2
source) groups operators in two layers:
- Its bison grammar `cal.y` (shipped as the generated `nseel-caltab.c`; the
  same declarations are in AVS's `evallib/cal.y`) declares
  `%left '|'`, `%left '&'`, `%left '+' '-'`, `%left '*' '/' '%'`,
  `%left UMINUS`. So `|` and `&` are *looser* than all arithmetic.
- `preprocessCode()` in `nseel-compiler.c` rewrites the rest as function
  calls by scanning the text, before the grammar sees it:
  - `&&`, `||` and the comparisons become `_and()`, `_equal()` and so on.
    Their operands run to the nearest `,` `;` `)`, so they are looser than
    any arithmetic, and `&&`/`||` are looser than the comparisons.
  - `^`, `%` and `!` take only the operand beside them, so they are tighter
    than everything, including a unary minus to their left.

**What eel-wasm does.** The operator table in `tools/buildParser.js` (marked
`TODO: Confirm these orders`) puts `&`, `|` and `&&`/`||` *tighter* than
everything, and `!` and unary minus at the level of binary `+`/`-`. `^` and
`%` already match.

| expression | MilkDrop 2 | eel-wasm 0.0.16 |
|---|---|---|
| `5 - 3 & 6` | 2 | 3 |
| `6 & 3 * 2` | 6 | 4 |
| `3 & 6 ^ 2` | 0 | 4 |
| `-3 & 5` | 5 | -1 |
| `!0 * 2` | 2 | 1 |
| `1 > 0 && 0 > 1` | 0 | 1 |
| `1 + 1 && 0` | 0 | 1 |
| `0 \|\| 3 > 2` | 1 | 0 |

The MilkDrop column comes from MilkDrop 2's own lexer, parse tables and
`preprocessCode()`, compiled unchanged with only the code generator replaced.
That build reproduces 122 of the 124 cases in `tools/testCases.ts` it can
evaluate. The two it misses are a `\\` comment (stripped by MilkDrop's preset
loader, not by ns-eel2) and division by zero. The existing cases never combine
`&`, `|`, `!` or `&&` with another operator, which is why the test preset
didn't catch this.

Even `buildTestPreset.js`'s own bookkeeping line,
`monitor = if(!pass && monitor == -1, …)`, relies on MilkDrop's grouping: under
the current grammar it parses as `(!(pass && monitor)) == -1`.

**Impact.** We checked 9,487 presets: the projectM `presets-milkdrop-original`,
`presets-cream-of-the-crop` and `presets-projectm-classic` collections. 110 of
them (1.2%) compute different values under 0.0.16's grouping than under
MilkDrop's, mostly through `&`. For example, `bass-new_bass&time*.63` means
`(bass-new_bass)&(time*.63)` to MilkDrop, but `bass-(new_bass&time)*.63` here.
With the change below, no grouping difference remains in that corpus.

**Fix.** Only the operator table and the `%prec` of unary `-`/`+` change. The
lexer, productions and actions don't, so no program that parses today stops
parsing. Only the grouping changes. The diff adds 16 cases to `testCases.ts`:
10 fail before the change, and the full suite passes after it.

```js
    ["right", "ASSIGNMENT_OPERATOR_TOKEN"],
    ["left", "LOGICAL_OPERATOR_TOKEN"],
    ["right", "COMPARISON_TOKEN"],
    ["left", "|"],
    ["left", "&"],
    ["left", "+", "-"],
    ["left", "*", "/"],
    ["right", "UMINUS"],
    ["left", "%", "^"],
    ["right", "!"],
    // ...
    ["- expression", unaryExpression, { prec: "UMINUS" }],
    ["+ expression", unaryExpression, { prec: "UMINUS" }],
```

Happy to open this as a PR. A preset holding only the precedence cases, in
`buildTestPreset.js`'s format, is attached, if you'd like to confirm it in
Winamp.

---

## PR title (if filed as a PR)

Match MilkDrop's operator precedence for `&`, `|`, `!`, `&&`/`||` and unary minus

## PR body

Fixes #<issue>. Changes the operator table in `tools/buildParser.js` to
MilkDrop 2's grouping (the reasoning is in a comment above the table), and
gives unary `-`/`+` an explicit `UMINUS` precedence. Adds 16 cases to
`tools/testCases.ts`. Regenerating `build/parser.js` changes only jison's LR
table.

`pnpm build` and `vitest run` pass, 255 tests including the new cases.

import parser from "@typescript-eslint/parser";
import reactHooks from "eslint-plugin-react-hooks";

/*
 * Lint, scoped to one class of bug.
 *
 * This project had no linter, and the cost of that arrived in v0.6.48: two
 * `useState` calls written below an `if (isLoading) return` meant the first
 * render registered fewer hooks than the second, React refused to reconcile,
 * and every detail page blanked the app with no way back. TypeScript cannot see
 * hook ordering, no test rendered the screen, and the rule that catches it in a
 * second is thirty years of nobody's opinion — it is mechanical.
 *
 * So: `react-hooks` rules, and deliberately nothing else. A style pass over a
 * codebase this size would produce hundreds of findings nobody asked for and
 * bury the one rule that matters under them, and a lint step people learn to
 * ignore protects nothing. Formatting is already settled by how the code is
 * written; correctness is what is missing.
 *
 * `exhaustive-deps` is a warning rather than an error on purpose. It is right
 * often enough to be worth reading and wrong often enough — the deliberate
 * mount-once effect, the ref that must not retrigger — that failing a build on
 * it would teach people to disable the plugin rather than to think.
 */
export default [
  {
    files: ["src/**/*.{ts,tsx}"],
    /*
     * Existing `eslint-disable` comments name rules this config does not turn
     * on — `no-var` in the test harnesses, for the global they each declare.
     * Reporting those as unused would add sixteen findings that say nothing
     * about this codebase and everything about the config being narrow.
     */
    linterOptions: { reportUnusedDisableDirectives: false },
    languageOptions: {
      parser,
      parserOptions: {
        ecmaVersion: "latest",
        sourceType: "module",
        ecmaFeatures: { jsx: true },
      },
    },
    plugins: { "react-hooks": reactHooks },
    rules: {
      // The one that would have caught the blank screen.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
    },
  },
];

/*
 * Escape closes the dialog a screen just opened, not the screen (R13-5).
 *
 * Search, Marked, Collections and others pass useBackHandler an inline
 * function, and own the open state of the dialogs they show. Opening one
 * re-renders the screen, and the hook's effect depended on `fn`, so it re-ran —
 * after the dialog's, because React runs a child's effects before its
 * parent's. The screen's handler went back on top of the dialog's, and Escape
 * navigated away with the dialog still standing. jsdom cannot show where
 * anything is, so this asserts which handler runs.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act, useState } from "react";
import { createRoot, type Root } from "react-dom/client";
import { FocusProvider, useBackHandler } from "./FocusController";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let calls: string[];
let open: () => void;
let rerender: () => void;

function Dialog({ onClose }: { onClose: () => void }) {
  useBackHandler(onClose);
  return <div>dialog</div>;
}

function Screen() {
  const [shown, setShown] = useState(false);
  const [tick, setTick] = useState(0);
  open = () => setShown(true);
  rerender = () => setTick((n) => n + 1);
  // Inline, as Search, Marked and Collections write it.
  useBackHandler(() => calls.push(`screen back@${tick}`));
  return shown ? (
    <Dialog
      onClose={() => {
        calls.push("dialog close");
        setShown(false);
      }}
    />
  ) : null;
}

function escape() {
  act(() => {
    document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
  });
}

beforeEach(async () => {
  calls = [];
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  await act(async () => {
    root.render(
      <FocusProvider>
        <Screen />
      </FocusProvider>,
    );
  });
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("Escape over a dialog a screen opened", () => {
  it("closes the dialog rather than running the screen's back", () => {
    act(() => open());
    escape();
    expect(calls).toEqual(["dialog close"]);
  });

  it("still closes the dialog after the screen renders again", () => {
    act(() => open());
    act(() => rerender());
    act(() => rerender());
    escape();
    expect(calls).toEqual(["dialog close"]);
  });

  it("goes back to the screen's handler once the dialog has closed", () => {
    act(() => open());
    escape();
    escape();
    expect(calls).toEqual(["dialog close", "screen back@0"]);
  });

  // The stable wrapper must still call what the latest render passed, or the
  // screen's back would act on the state it had when it mounted.
  it("runs the screen's latest handler, not the one it mounted with", () => {
    act(() => rerender());
    act(() => rerender());
    escape();
    expect(calls).toEqual(["screen back@2"]);
  });
});

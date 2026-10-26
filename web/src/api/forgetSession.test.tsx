/*
 * A session that ends any way but signing out must not leave its cache behind.
 *
 * Found 2026-10-26: after a restore (which ends every session on the server),
 * the app put the sign-in page up over a full cache, and once signed in again
 * the rail showed a library under the name the restore had just taken away.
 * The same thing follows a password change or "sign out every other device",
 * and then whoever signs in next is shown the last person's lists.
 */
import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useForgetEndedSession } from "./hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let qc: QueryClient;

function Gate({ user }: { user: string | undefined }) {
  useForgetEndedSession(user);
  return null;
}

function seed() {
  qc.setQueryData(["libraries"], [{ id: 1, name: "Restore test" }]);
  qc.setQueryData(["items", "infinite", 1], { pages: [] });
  qc.setQueryData(["auth-status"], { authenticated: false });
  qc.setQueryData(["login-accounts"], { accounts: [] });
}

function render(user: string | undefined) {
  act(() => {
    root.render(
      <QueryClientProvider client={qc}>
        <Gate user={user} />
      </QueryClientProvider>,
    );
  });
}

const cached = () => qc.getQueryCache().getAll().map((q) => q.queryKey[0]).sort();

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  qc = new QueryClient();
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
});

describe("useForgetEndedSession", () => {
  it("forgets everything but auth and the sign-in list when the session ends", () => {
    render("u1");
    seed();
    render(undefined);
    expect(cached()).toEqual(["auth-status", "login-accounts"]);
  });

  it("forgets when a different account is signed in", () => {
    render("u1");
    seed();
    render("u2");
    expect(cached()).toEqual(["auth-status", "login-accounts"]);
  });

  it("keeps the cache while the same account stays signed in", () => {
    render("u1");
    seed();
    render("u1");
    expect(cached()).toContain("libraries");
  });

  it("has nothing to forget on a cold start", () => {
    render(undefined);
    seed();
    render("u1");
    expect(cached()).toContain("libraries");
  });
});

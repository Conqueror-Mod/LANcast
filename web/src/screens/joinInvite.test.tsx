/*
 * Joining with an invite (ADR 0104): the page reads the secret from the
 * fragment, takes it out of the address bar, checks it, and makes the account.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { JoinWithInvite } from "./Auth";
import { useAuthStatus } from "@/api/hooks";

declare global {
  // eslint-disable-next-line no-var
  var IS_REACT_ACT_ENVIRONMENT: boolean;
}
globalThis.IS_REACT_ACT_ENVIRONMENT = true;

let host: HTMLDivElement;
let root: Root;
let sent: { url: string; body: Record<string, string> }[];
let inviteWorks: boolean;
let replaced: string[];
let redeemed: boolean;

beforeEach(() => {
  host = document.createElement("div");
  document.body.append(host);
  root = createRoot(host);
  sent = [];
  inviteWorks = true;
  replaced = [];
  redeemed = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      const body = init?.body ? JSON.parse(init.body as string) : {};
      sent.push({ url, body });
      const json = (v: unknown, status = 200) =>
        new Response(JSON.stringify(v), { status, headers: { "Content-Type": "application/json" } });
      if (!inviteWorks) {
        return json({ error: { code: "not_found", message: "this invite has been used, withdrawn, or has expired" } }, 404);
      }
      if (url.endsWith("/api/auth/status")) return json({ configured: true, authenticated: redeemed });
      if (url.endsWith("/api/invites/check")) return json({ from: "Dana", expires_at: 2000000000 });
      if (url.endsWith("/api/invites/redeem")) {
        if (body.username === "taken") {
          return json({ error: { code: "conflict", message: "that name is taken; choose another" } }, 409);
        }
        redeemed = true;
        return json({ id: "u_9", name: body.username, role: "member" }, 201);
      }
      return json({});
    }),
  );
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.history.replaceState(null, "", "/");
});

async function settle() {
  for (let i = 0; i < 4; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 5));
    });
  }
}

async function open(hash: string) {
  window.history.replaceState(null, "", "/join" + hash);
  await act(async () => {
    root.render(
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <JoinWithInvite onJoined={() => replaced.push("/")} />
      </QueryClientProvider>,
    );
  });
  await settle();
}

function fill(label: string, value: string) {
  const input = [...host.querySelectorAll("label")]
    .find((l) => l.textContent?.startsWith(label))!
    .querySelector("input") as HTMLInputElement;
  const set = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")!.set!;
  act(() => {
    set.call(input, value);
    input.dispatchEvent(new Event("input", { bubbles: true }));
  });
}

async function submit() {
  await act(async () => {
    host.querySelector("form")!.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  });
  await settle();
}

describe("joining with an invite", () => {
  it("takes the secret out of the address bar and sends it only in a body", async () => {
    await open("#s3cr3t-token");
    expect(window.location.hash).toBe("");
    expect(window.location.pathname).toBe("/join");
    expect(sent[0].url).toBe("/api/invites/check");
    expect(sent[0].body).toEqual({ token: "s3cr3t-token" });
    expect(sent.every((s) => !s.url.includes("s3cr3t"))).toBe(true);
    expect(host.textContent).toContain("Join Dana's LANcast");
  });

  it("makes the account and opens the library as them", async () => {
    await open("#tok");
    fill("Your name", "  georgia ");
    fill("Password", "her own good password");
    fill("Password again", "her own good password");
    await submit();
    const redeem = sent.find((s) => s.url.endsWith("/redeem"))!;
    expect(redeem.body).toEqual({ token: "tok", username: "georgia", password: "her own good password" });
    expect(replaced).toEqual(["/"]);
  });

  it("catches two different passwords before asking the server", async () => {
    await open("#tok");
    fill("Your name", "georgia");
    fill("Password", "one good password");
    fill("Password again", "another good password");
    await submit();
    expect(host.querySelector('[role="alert"]')?.textContent).toBe("The two passwords are different.");
    expect(sent.some((s) => s.url.endsWith("/redeem"))).toBe(false);
  });

  it("says a taken name and lets them try another", async () => {
    await open("#tok");
    fill("Your name", "taken");
    fill("Password", "a good long password");
    fill("Password again", "a good long password");
    await submit();
    expect(host.querySelector('[role="alert"]')?.textContent).toContain("that name is taken");
    expect(replaced).toEqual([]);
    expect(host.querySelector("form")).toBeTruthy();
  });

  it("says a dead link plainly, and asks for a new one", async () => {
    inviteWorks = false;
    await open("#old");
    expect(host.textContent).toContain("This invite can't be used");
    expect(host.textContent).toContain("This invite has been used, withdrawn, or has expired. Ask whoever sent it");
  });

  it("reads a link pasted into a tab already on /join", async () => {
    await open("");
    const reload = vi.fn();
    vi.spyOn(window, "location", "get").mockReturnValue({ ...window.location, reload } as Location);
    await act(async () => {
      window.dispatchEvent(new HashChangeEvent("hashchange"));
    });
    expect(reload).toHaveBeenCalled();
  });
  it("says a link with no secret is incomplete, without asking the server", async () => {
    await open("");
    expect(host.textContent).toContain("missing the part that makes it work");
    expect(sent).toEqual([]);
  });
});

/*
 * The app's own gate around the page: signed out shows the join page, signed
 * in shows the app. Redeeming used to refresh the auth status, which flipped
 * this gate before the page could leave, and a friend who had just joined was
 * told they were "already signed in" and that the link was for somebody new.
 */
function Gate({ onJoined }: { onJoined: () => void }) {
  const { data } = useAuthStatus();
  if (!data) return null;
  return data.authenticated ? <p className="signed-in-app">the app</p> : <JoinWithInvite onJoined={onJoined} />;
}

describe("joining, inside the sign-in gate", () => {
  it("leaves for the library before the gate can show the signed-in app", async () => {
    window.history.replaceState(null, "", "/join#tok");
    await act(async () => {
      root.render(
        <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
          <Gate onJoined={() => replaced.push("/")} />
        </QueryClientProvider>,
      );
    });
    await settle();
    fill("Your name", "georgia");
    fill("Password", "her own good password");
    fill("Password again", "her own good password");
    await submit();
    expect(replaced).toEqual(["/"]);
    expect(host.querySelector(".signed-in-app")).toBeNull();
  });
});
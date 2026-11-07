import { RemotePanel } from "@/components/RemotePanel";

/*
 * Devices: your other screens, as a remote (ADR 0117).
 *
 * The player and an item page each open the same panel to *send* something.
 * This page is for the moment after: you sent a film to the TV from your
 * phone, went back to browsing, and now want to pause it. It sends nothing and
 * only controls.
 */
export function Devices() {
  return (
    <div className="browse devices">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Devices</h1>
      </div>
      <p className="devices__note">
        Your other devices signed in as you, and what each is playing. Send something to one
        from its page or from the player.
      </p>
      <RemotePanel page />
    </div>
  );
}

import { useEffect, useState } from "react";
import { useCurrentUser, useRenameSelf } from "@/api/hooks";

// Admin-only. Members never mount this, so its admin-only queries never fire.
/*
 * Your own display name.
 *
 * Separate from the Users pane on purpose: renaming yourself affects nobody and
 * needs no role, while renaming somebody else is administration. Two surfaces
 * because they answer to different authority, not because the form is different.
 *
 * The account id does not change, so watch history, ratings and playlist
 * membership follow silently — which is the whole reason this is a rename
 * rather than "delete and make a new one".
 */
function DisplayNameForm() {
  const me = useCurrentUser();
  const rename = useRenameSelf();
  const [name, setName] = useState("");
  const [saved, setSaved] = useState(false);

  // Seeded from the current name once it arrives, rather than left empty: an
  // empty box beside "Display name" reads as though the name has been lost.
  useEffect(() => {
    if (me?.name) setName(me.name);
  }, [me?.name]);

  const unchanged = !name.trim() || name.trim() === me?.name;

  return (
    <form
      className="set-add"
      onSubmit={(e) => {
        e.preventDefault();
        rename.mutate(name.trim(), { onSuccess: () => setSaved(true) });
      }}
    >
      <span className="set-sublabel">Display name</span>
      <input
        className="set-input"
        value={name}
        maxLength={60}
        onChange={(e) => {
          setName(e.target.value);
          setSaved(false);
        }}
        aria-label="Display name"
      />
      <button className="set-btn" disabled={unchanged || rename.isPending}>
        {rename.isPending ? "Saving…" : "Save name"}
      </button>
      {saved && <span className="set-note">Saved.</span>}
      {rename.isError && (
        <span className="set-error">{(rename.error as Error).message}</span>
      )}
    </form>
  );
}

// Everyone can change their own password. The server revokes the caller's
// sessions on success, so the auth gate returns them to the login screen.
/*
 * Your account, in four panes (docs/settings-plan.md).
 *
 * One "Account" pane held a name, a password, passkeys, API keys, languages,
 * sharing and history, and sat under "This device" although every one of them
 * follows the person to any device. Split by what somebody comes looking for.
 */
export function ProfileSection() {
  return (
    <section className="settings__section">
      <span className="section-label">Profile</span>
      <DisplayNameForm />
    </section>
  );
}

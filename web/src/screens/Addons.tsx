import { Link } from "react-router-dom";
import { usePlugins, useIsAdmin } from "@/api/hooks";
import "./Addons.css";

/*
 * The add-ons page.
 *
 * The rail has listed Add-ons as a destination since the shell was built, with
 * a comment explaining why it belongs there — "it is a *place*, a thing with
 * contents you go and look at, and the rail is where places live" — and then
 * pointed it at `/settings?pane=addons`. That is the mismatch this closes: a
 * rail entry that lands in Settings teaches that Add-ons is a setting.
 *
 * What it is *not* is a store. Add-on distribution is M4 territory and the
 * plugin runtime is deliberately last (the roadmap's ordering principle: the
 * plugin contract waits for one full build of the core). So this page is
 * honest about being early — it lists what is installed, explains what an
 * add-on can and cannot do here, and sends installation to the pane that
 * already does it properly rather than growing a second uploader that would
 * have to be kept in step.
 *
 * A page that says "nothing here yet, and here is what will be" is more useful
 * than no page, because the question it answers — "what else can this thing
 * do?" — is one people ask on their first evening.
 */
export function Addons() {
  const { data: plugins, isError } = usePlugins();
  const isAdmin = useIsAdmin();
  const installed = plugins ?? [];

  return (
    <div className="browse addons">
      <div className="browse__head browse__head--sticky">
        <h1 className="browse__title">Add-ons</h1>
        <span className="browse__count">{installed.length || ""}</span>
      </div>

      {/*
        A member reaching this by URL cannot be shown the list: /api/plugins is
        admin-gated on the server, because what is installed and what it has
        been granted is operator information. Saying so beats the alternative,
        which is the empty state below telling them nothing is installed when
        the truth is that nobody asked them.
      */}
      {isError && !isAdmin ? (
        <p className="addons__empty">
          Add-ons are managed by whoever administers this server.
        </p>
      ) : installed.length > 0 ? (
        <div className="addons__list">
          {installed.map((p) => (
            <div className="addons__row" key={p.name}>
              <div className="addons__what">
                <span className="addons__name">{p.name}</span>
                <span className="addons__version">v{p.version}</span>
              </div>
              {/* Trust is the whole security story for add-ons, so it is the
                  thing the row says rather than something to go and look up. */}
              <span
                className={
                  "addons__state" + (p.enabled ? " is-on" : "")
                }
              >
                {p.enabled ? "Enabled" : "Disabled"}
              </span>
            </div>
          ))}
        </div>
      ) : (
        <div className="addons__empty">
          <p className="addons__lead">Nothing installed yet.</p>
          <p>
            An add-on extends the server — a metadata provider that knows a
            catalogue LANcast does not, a scanner for a format it cannot read.
            They run on the server, with permissions you grant one at a time.
          </p>
          <p className="addons__aside">
            The add-on runtime is deliberately one of the last things this
            project builds. A plugin contract is a promise about the shape of
            the core, and promising that before the core is finished is how a
            plugin API becomes the thing that stops the core changing.
          </p>
        </div>
      )}

      {/* Installing is an admin act — it is code running on the server — so a
          non-admin gets the list and not the door. */}
      {isAdmin && (
        <Link className="addons__manage" to="/settings?pane=addons">
          Install and manage add-ons →
        </Link>
      )}
    </div>
  );
}

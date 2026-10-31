import { useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { apiGet, apiPost } from "@/api/client";
import { useBackHandler } from "@/focus/FocusController";
import { errorMessage } from "@/lib/errors";
import "./AddToPlaylist.css";
import "./ReportIssue.css";

/*
 * Report an issue (ADR 0109): written here, sent by the server to the
 * developer when, and only when, Send is pressed.
 *
 * It replaced a mailto link, which in the desktop window opened Windows'
 * "pick an app" box and then went nowhere, and which could never carry a
 * log. Nothing here leaves the app: no browser, no mail program.
 *
 * What goes with the report is said beside the button, and the log, which
 * only an administrator can attach, can be read in full before it is sent.
 */

interface ReportInfo {
  can_send: boolean;
  can_attach_log: boolean;
}

// Which client this is, as the report names it. The desktop window binds
// lancastClientNote; a browser has none.
function clientName(): string {
  if (typeof window.lancastClientNote === "function") return "desktop window";
  return window.matchMedia?.("(max-width: 600px)").matches ? "phone browser" : "browser";
}

export function ReportIssue({ version, onClose }: { version?: string; onClose: () => void }) {
  useBackHandler(onClose);
  const info = useQuery({
    queryKey: ["report-info"],
    queryFn: ({ signal }) => apiGet<ReportInfo>("/api/report", signal),
    staleTime: 60_000,
  });
  const [what, setWhat] = useState("");
  const [expected, setExpected] = useState("");
  const [steps, setSteps] = useState("");
  const [contact, setContact] = useState("");
  const [includeLog, setIncludeLog] = useState(false);
  const [showLog, setShowLog] = useState(false);
  const [copied, setCopied] = useState(false);

  const log = useQuery({
    queryKey: ["report-log"],
    queryFn: ({ signal }) => apiGet<{ lines: string[] }>("/api/logs?lines=2000", signal),
    enabled: showLog && !!info.data?.can_attach_log,
  });

  const send = useMutation({
    mutationFn: () =>
      apiPost<{ sent: boolean }>("/api/report", {
        what,
        expected,
        steps,
        contact,
        client: clientName(),
        include_log: includeLog && !!info.data?.can_attach_log,
      }),
  });

  const canSend = !!info.data?.can_send;
  const ready = what.trim() !== "" && !send.isPending;

  const asText = () =>
    [
      "What happened:",
      what.trim(),
      "",
      "What you expected:",
      expected.trim(),
      "",
      "Steps to make it happen again:",
      steps.trim(),
      "",
      `LANcast ${version || "unknown"} · ${clientName()}`,
    ].join("\n");

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(asText());
      setCopied(true);
    } catch {
      setCopied(false);
    }
  };

  if (send.isSuccess) {
    return (
      <div className="addpl__overlay" onClick={onClose}>
        <div className="addpl report" role="dialog" aria-label="Report sent" onClick={(e) => e.stopPropagation()}>
          <div className="addpl__head">
            <span className="section-label">Report an issue</span>
            <button className="addpl__x" onClick={onClose} aria-label="Close">
              ✕
            </button>
          </div>
          <p className="report__done">Sent. Thank you for taking the time.</p>
          <div className="report__actions">
            <button className="addpl__create" onClick={onClose} autoFocus>
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="addpl__overlay" onClick={onClose}>
      <div
        className="addpl report"
        role="dialog"
        aria-label="Report an issue"
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => {
          if (e.key === "Escape") onClose();
        }}
      >
        <div className="addpl__head">
          <span className="section-label">Report an issue</span>
          <button className="addpl__x" onClick={onClose} aria-label="Close">
            ✕
          </button>
        </div>

        <label className="report__field">
          <span>What happened</span>
          <textarea
            className="addpl__input report__text"
            value={what}
            rows={4}
            autoFocus
            maxLength={4000}
            onChange={(e) => setWhat(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>What you expected</span>
          <textarea
            className="addpl__input report__text"
            value={expected}
            rows={2}
            maxLength={4000}
            onChange={(e) => setExpected(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>Steps to make it happen again</span>
          <textarea
            className="addpl__input report__text"
            value={steps}
            rows={3}
            maxLength={4000}
            onChange={(e) => setSteps(e.target.value)}
          />
        </label>
        <label className="report__field">
          <span>How to reach you, if you'd like a reply (optional)</span>
          <input
            className="addpl__input"
            value={contact}
            maxLength={200}
            onChange={(e) => setContact(e.target.value)}
          />
        </label>

        {info.data?.can_attach_log && (
          <div className="report__log">
            <label className="report__check">
              <input type="checkbox" checked={includeLog} onChange={(e) => setIncludeLog(e.target.checked)} />
              Include the server log (the last 2,000 lines)
            </label>
            <p className="report__note">
              The log names this server's folders, addresses and accounts.{" "}
              <button type="button" className="report__link" onClick={() => setShowLog((v) => !v)}>
                {showLog ? "Hide it" : "Read it first"}
              </button>
            </p>
            {showLog && (
              <pre className="report__logview" aria-label="The log that would be sent">
                {log.isLoading ? "Loading…" : log.isError ? "The log could not be read." : (log.data?.lines ?? []).join("\n")}
              </pre>
            )}
          </div>
        )}

        {info.isError && <p className="addpl__error">{errorMessage(info.error)}</p>}
        {send.isError && <p className="addpl__error">{errorMessage(send.error)}</p>}

        {info.data && !canSend ? (
          <p className="report__note">
            This build of LANcast can't send reports. Copy the report and send it to the developer
            another way.
          </p>
        ) : (
          <p className="report__note">
            Goes to the developer, with this LANcast's version, its operating system, which app you're
            using, and whether you're an administrator. Nothing is sent until you press Send.
          </p>
        )}

        <div className="report__actions">
          <button type="button" className="addpl__create report__copy" onClick={copy} disabled={what.trim() === ""}>
            {copied ? "Copied" : "Copy report"}
          </button>
          {canSend && (
            <button type="button" className="addpl__create" onClick={() => send.mutate()} disabled={!ready}>
              {send.isPending ? "Sending…" : "Send"}
            </button>
          )}
        </div>
      </div>
    </div>
  );
}

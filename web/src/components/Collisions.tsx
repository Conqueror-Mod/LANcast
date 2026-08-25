import { useState } from "react";
import { useCollisions, useCompareCollision } from "@/api/hooks";
import { useFocusable } from "@/focus/FocusController";
import type { Collision, CollisionMember } from "@/api/types";
import "./Collisions.css";

/*
 * Two files claiming one work (ADR 0042).
 *
 * This screen's whole job is to *not* decide. There is no merge button, no
 * "keep this one", and no delete — not as a missing feature but as the
 * decision. A shared provider id is evidence that something wants a human, and
 * of the thirteen pairs in the library this was built against, two were not
 * duplicates at all: a film split across two discs, and a 1989 film wearing a
 * 2022 film's identity from a stale `.nfo`. A server that resolved these would
 * have hidden all three problems.
 *
 * So it shows the evidence and stops: both paths, both sizes, what each file
 * claimed to be, and — on request — whether the bytes match.
 */

function bytes(n: number | null): string {
  if (n == null) return "size unknown";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let u = 0;
  while (v >= 1024 && u < units.length - 1) {
    v /= 1024;
    u++;
  }
  // One decimal past kilobytes: "2.6 GB" is the number people compare, and
  // "2,832,374,353 B" is the number that proves it. Both are shown — this one
  // for reading, the exact byte count in the title attribute for checking.
  return `${u === 0 ? v : v.toFixed(1)} ${units[u]}`;
}

function MemberRow({ member }: { member: CollisionMember }) {
  return (
    <li className="collide__file">
      <div className="collide__file-head">
        {/* What the filename claimed, when it claimed anything. Rendered as a
            quotation rather than a fact, because it is one: the motivating file
            said "Alternate Cut" and was a byte-for-byte copy. */}
        {member.edition && (
          <span className="collide__edition">{member.edition}</span>
        )}
        <span className="collide__size" title={
          member.size_bytes == null
            ? undefined
            : `${member.size_bytes.toLocaleString()} bytes`
        }>
          {bytes(member.size_bytes)}
        </span>
        {member.missing && <span className="collide__missing">file missing</span>}
      </div>
      {/* Selectable, and the one place the API returns a path. Somebody reading
          this is about to go and look at the file. */}
      <code className="collide__path">{member.path}</code>
      {member.unreadable && (
        <span className="collide__unreadable">
          Could not be read — no comparison possible
        </span>
      )}
      {member.fingerprint && (
        <code className="collide__hash" title={member.fingerprint}>
          {member.fingerprint.slice(0, 16)}…
        </code>
      )}
    </li>
  );
}

function CollisionCard({ collision }: { collision: Collision }) {
  const [comparing, setComparing] = useState(false);
  const { data, isFetching } = useCompareCollision(
    comparing ? collision.external_id : null,
  );
  const compared = data?.collisions.find(
    (c) => c.external_id === collision.external_id,
  );
  const shown = compared ?? collision;
  const compare = useFocusable(() => setComparing(true));

  return (
    <div className="collide">
      <div className="collide__head">
        <span className="collide__title">{collision.members[0]?.title}</span>
        <span className="collide__count">
          {collision.members.length} files
        </span>
      </div>

      <ul className="collide__files">
        {shown.members.map((m) => (
          <MemberRow key={m.id} member={m} />
        ))}
      </ul>

      <div className="collide__verdict">
        {/*
          Size first, because it is free and the negative answer is the strong
          one: different sizes rule out a copy outright, where equal sizes only
          make one likely.
        */}
        <span
          className={
            "collide__flag" +
            (shown.same_size ? " collide__flag--same" : " collide__flag--differ")
          }
        >
          {shown.same_size ? "Same size" : "Different sizes"}
        </span>

        {shown.same_bytes === undefined ? (
          <button
            {...compare}
            type="button"
            className="collide__compare"
            disabled={isFetching}
            onClick={() => setComparing(true)}
          >
            {isFetching ? "Reading…" : "Compare bytes"}
          </button>
        ) : (
          <span
            className={
              "collide__flag" +
              (shown.same_bytes
                ? " collide__flag--same"
                : " collide__flag--differ")
            }
          >
            {/*
              "so far as sampled" is not hedging, it is the claim. The
              comparison reads the size and three 1 MB windows, which cannot
              prove equality — and saying "identical" would be the report
              asserting more than it checked. It is a defensible shortcut only
              because nothing here acts on the answer.
            */}
            {shown.same_bytes
              ? "Identical, so far as sampled"
              : "Contents differ"}
          </span>
        )}
      </div>
    </div>
  );
}

export function Collisions({ enabled }: { enabled: boolean }) {
  const { data, isLoading } = useCollisions(enabled);
  const collisions = data?.collisions ?? [];

  if (!enabled || isLoading || collisions.length === 0) return null;

  return (
    <section className="collide-section">
      <div className="review__head">
        <span className="section-label">Two files, one work</span>
        <span className="review__rule" />
        <span className="review__count">{collisions.length}</span>
      </div>
      <p className="review__lead">
        These works are claimed by more than one file. LANcast does not merge,
        rank or delete them — a shared identity can mean a redundant copy, a
        second edition, one film in two parts, or a file that is simply wrong
        about what it is, and only you can tell which.
      </p>
      <div className="collide-list">
        {collisions.map((c) => (
          <CollisionCard key={`${c.provider}:${c.external_id}`} collision={c} />
        ))}
      </div>
    </section>
  );
}

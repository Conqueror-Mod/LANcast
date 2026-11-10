import { useNavigate } from "react-router";
import { useResume } from "@/api/hooks";
import { useBackHandler } from "@/focus/FocusController";
import { ResumeCardRow } from "@/components/ResumeConsole";
import { useContinueMenu } from "@/lib/useContinueMenu";
import "@/components/ResumeConsole.css";

/*
 * See all, from the home page's resume console (ADR 0118 §2): everything in
 * progress, every kind, with no two-of-a-kind cap. The same cards and the same
 * menus as the console, so an entry cannot mean one thing there and another
 * here.
 */
export function ContinueAll() {
  const navigate = useNavigate();
  const { data, isLoading, isError } = useResume(true);
  const { resumeActions, resumeOpen } = useContinueMenu();
  const back = () => navigate("/");
  useBackHandler(back);
  const cards = data?.cards ?? [];

  return (
    <div className="browse">
      <div className="browse__head browse__head--sticky">
        <button className="pl-back" onClick={back}>
          ← Home
        </button>
        <h1 className="browse__title">Continue</h1>
        <span className="browse__count">{cards.length ? cards.length.toLocaleString() : ""}</span>
      </div>
      {isError && <p className="browse__message">This list could not be loaded.</p>}
      {!isLoading && !isError && cards.length === 0 && (
        <p className="browse__message">Nothing in progress. Whatever you start shows up here.</p>
      )}
      <ol className="resume__list resume__list--all">
        {cards.map((c) => (
          <li key={`${c.kind}-${c.item.id}`} className="resume__item">
            <ResumeCardRow card={c} actions={resumeActions} open={resumeOpen} />
          </li>
        ))}
      </ol>
    </div>
  );
}

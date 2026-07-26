import { useNavigate } from "react-router-dom";

// A placeholder for screens not yet built in this slice (Detail, Player,
// Settings). Honest about being unfinished rather than faking a screen.
export function Stub({ name, note }: { name: string; note: string }) {
  const navigate = useNavigate();
  return (
    <div style={{ padding: "60px 0", maxWidth: 520 }}>
      <span className="section-label">{name}</span>
      <p style={{ color: "var(--text-secondary)", marginTop: 16, lineHeight: 1.6 }}>
        {note}
      </p>
      <button
        onClick={() => navigate(-1)}
        style={{
          marginTop: 24,
          padding: "8px 16px",
          borderRadius: "var(--radius-tile)",
          border: "1px solid var(--gold-dormant)",
          background: "none",
          color: "var(--text-primary)",
          cursor: "pointer",
        }}
      >
        Back
      </button>
    </div>
  );
}

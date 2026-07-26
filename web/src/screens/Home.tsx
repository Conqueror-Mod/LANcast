import { Navigate } from "react-router-dom";
import { useLibraries } from "@/api/hooks";

// Home will become the hub shelves (continue watching → recently added →
// per-library) from design.md. For this slice it lands on the first library's
// grid so the app opens on content; the nav exposes the rest.
export function Home() {
  const { data: libraries, isLoading } = useLibraries();

  if (isLoading) return null;
  if (libraries && libraries.length > 0) {
    return <Navigate to={`/library/${libraries[0].id}`} replace />;
  }
  return (
    <p style={{ color: "var(--text-muted)", padding: "40px 0" }}>
      No libraries yet. Add one from Settings.
    </p>
  );
}

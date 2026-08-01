import { useParams } from "react-router-dom";
import { useLibraries } from "@/api/hooks";
import { LibraryView } from "./LibraryView";
import { configForKind } from "./libraryConfig";
import "./Browse.css";

// Dispatcher: resolve the library, then render the shared LibraryView with the
// configuration for its kind. Movie and TV libraries diverge only in config, not
// in a separate screen.
export function Browse() {
  const { id } = useParams();
  const libraryID = Number(id);
  const { data: libraries } = useLibraries();
  const library = libraries?.find((l) => l.id === libraryID);

  // Libraries not loaded yet, or an unknown id. Once loaded and still missing,
  // say so rather than render an empty grid against a phantom library.
  if (!library) {
    if (!libraries) return null;
    return <p className="browse__message">No such library.</p>;
  }

  return <LibraryView library={library} config={configForKind(library.kind)} />;
}

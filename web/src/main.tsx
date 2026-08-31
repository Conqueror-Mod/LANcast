import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import {
  QueryClient,
  QueryClientProvider,
  QueryCache,
} from "@tanstack/react-query";
import { FocusProvider } from "@/focus/FocusController";
import { ApiFailure } from "@/api/client";
import { App } from "@/App";
import { Splash } from "@/components/Splash";
import "@/styles/global.css";

// A 401 anywhere means the session lapsed or was revoked (a password change, an
// admin reset, an expiry). Invalidating the auth status re-runs the gate in
// App, which drops the user back to the login screen instead of leaving them
// staring at a half-broken library of failed requests.
const queryCache = new QueryCache({
  onError: (error) => {
    if (error instanceof ApiFailure && error.status === 401) {
      queryClient.invalidateQueries({ queryKey: ["auth-status"] });
    }
  },
});

const queryClient = new QueryClient({
  queryCache,
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <FocusProvider>
          <div className="nebula-field" aria-hidden="true" />
          <div className="starfield" aria-hidden="true" />
          <App />
          <Splash />
        </FocusProvider>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);

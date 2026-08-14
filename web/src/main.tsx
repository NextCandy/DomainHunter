import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { DensityProvider } from "./components/ui";
import { ThemeProvider } from "./lib/theme";
import { LocaleProvider } from "./lib/i18n";
import "./index.css";

const container = document.getElementById("root");
if (!container) throw new Error("找不到 #root 挂载点");

createRoot(container).render(
  <StrictMode>
    <ThemeProvider>
      <DensityProvider>
        <LocaleProvider>
          <BrowserRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
            <App />
          </BrowserRouter>
        </LocaleProvider>
      </DensityProvider>
    </ThemeProvider>
  </StrictMode>,
);

if (import.meta.env.PROD && "serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    void navigator.serviceWorker.register("/sw.js").catch(() => {
      // Offline enhancement is optional; the application itself remains usable.
    });
  });
}

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import App from "./App";
import { DensityProvider } from "./components/ui";
import { ThemeProvider } from "./lib/theme";
import "./index.css";

const container = document.getElementById("root");
if (!container) throw new Error("找不到 #root 挂载点");

createRoot(container).render(
  <StrictMode>
    <ThemeProvider>
      <DensityProvider>
        <BrowserRouter>
          <App />
        </BrowserRouter>
      </DensityProvider>
    </ThemeProvider>
  </StrictMode>,
);

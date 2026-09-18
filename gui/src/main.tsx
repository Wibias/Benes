/** Benes dashboard client for the Go proxy (`internal/server`). */
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { LanguageProvider } from "./i18n/hooks";
import App from "./App";
import "./styles.css";
/**
 * Component-owned workspace sheets. The aggregate stylesheet above still
 * describes the dashboard chrome that the workspace reuses, so the workspace's
 * own sheets load last and only override what the Shell actually owns.
 */
import "./styles/provider-workspace-shell.css";
import "./styles/provider-workspace-rail.css";
import "./styles/provider-workspace-detail.css";
import "./styles/codex-account-pool.css";

/** Id of the one mount point the Vite document ships. */
const ROOT_ELEMENT_ID = "root";

/**
 * A missing mount point means the bundle and the HTML it was built for disagree,
 * which is a build fault rather than a user state, so it fails with a readable
 * reason instead of React's null-target message.
 */
function requireMountPoint(): HTMLElement {
  const root = document.getElementById(ROOT_ELEMENT_ID);
  if (root === null) throw new Error("benes dashboard: #root is missing");
  return root;
}

/**
 * One language provider wraps the whole shell, so every page resolves copy from
 * the same locale state. StrictMode stays outside it to keep the dev double-render
 * covering provider and pages alike.
 *
 * The tree is rendered inline rather than through a local component: a component
 * defined in the entry file has no export to refresh against, which the dashboard's
 * fast-refresh lint rule rejects.
 */
createRoot(requireMountPoint()).render(
  <StrictMode>
    <LanguageProvider>
      <App />
    </LanguageProvider>
  </StrictMode>,
);

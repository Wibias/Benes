import type { Page } from "./app-routing";
import ErrorBoundary from "./components/ErrorBoundary";
import Dashboard from "./pages/Dashboard";
import Harnesses from "./pages/harnesses/Harnesses";
import ApiKeys from "./pages/ApiKeys";
import Logs from "./pages/Logs";
import Models from "./pages/Models";
import Providers from "./pages/Providers";
import Routing from "./pages/Routing";
import Sessions from "./pages/Sessions";
import Startup from "./pages/Startup";
import Storage from "./pages/Storage";
import Subagents from "./pages/Subagents";
import Tasks from "./pages/Tasks";
import Usage from "./pages/Usage";
import { PAGE_TKEY } from "./app-shell";
import type { TFn } from "./i18n/shared";

function AppPage({
  page,
  apiBase,
  codexRestartEpoch,
}: {
  page: Page;
  apiBase: string;
  codexRestartEpoch: number;
}) {
  switch (page) {
    case "dashboard": return <Dashboard apiBase={apiBase} />;
    case "startup": return <Startup apiBase={apiBase} restartEpoch={codexRestartEpoch} />;
    case "routing": return <Routing apiBase={apiBase} restartEpoch={codexRestartEpoch} />;
    case "providers": return <Providers apiBase={apiBase} />;
    case "models": return <Models key={apiBase} apiBase={apiBase} restartEpoch={codexRestartEpoch} />;
    case "subagents": return <Subagents key={apiBase} apiBase={apiBase} />;
    case "sessions": return <Sessions apiBase={apiBase} />;
    case "logs": return <Logs apiBase={apiBase} />;
    case "usage": return <Usage apiBase={apiBase} />;
    case "tasks": return <Tasks apiBase={apiBase} />;
    case "storage": return <Storage apiBase={apiBase} />;
    case "api": return <ApiKeys apiBase={apiBase} />;
    case "integrations": return <Harnesses apiBase={apiBase} />;
    case "harnesses": return <Harnesses apiBase={apiBase} />;
  }
}

export function AppPageBoundary({
  page,
  apiBase,
  codexRestartEpoch,
  t,
}: {
  page: Page;
  apiBase: string;
  codexRestartEpoch: number;
  t: TFn;
}) {
  return (
    <ErrorBoundary
      key={page}
      pageName={t(PAGE_TKEY[page])}
      title={t("errorBoundary.title")}
      message={t("errorBoundary.message")}
      detailsLabel={t("errorBoundary.details")}
      reloadLabel={t("errorBoundary.reload")}
    >
      <AppPage page={page} apiBase={apiBase} codexRestartEpoch={codexRestartEpoch} />
    </ErrorBoundary>
  );
}

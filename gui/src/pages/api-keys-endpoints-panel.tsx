/**
 * Benes dashboard source. The Endpoints panel of the API workspace.
 *
 * The listener ships its routes and its inbound-header rules, and this panel
 * draws them. The route list follows the accepted `api-access/endpoints` order,
 * the header columns follow the accepted `api-access/auth-matrix` binding, and
 * the three verdicts are named from one label table — so a header or a verdict
 * the listener could not send is not something this file can spell either.
 *
 * Every sentence is resolved before the markup, so the panel reads as one board
 * instead of a tree of calls, and no verdict is decided here: acceptance is the
 * Go data plane's rule and the owner rejects an unknown vocabulary at the
 * boundary before this component is reached.
 */
import { useT, type TKey } from "../i18n/shared.ts";
import { AUTH_MATRIX_COLUMNS, type ApiAuthDisposition, type ApiAuthMatrixRow } from "../api-access/auth-matrix.ts";
import { API_SURFACES, type ApiEndpointInfo } from "../api-access/endpoints.ts";
import { EndpointCopyIcon } from "../components/apikeys-workspace/copy-affordance.tsx";

/** What each surface is called, keyed by the owner's own surface list. */
const ROUTE_LABEL: Record<(typeof API_SURFACES)[number], TKey> = {
  responses: "api.responsesEndpoint",
  chatCompletions: "api.chatCompletionsEndpoint",
  messages: "api.messagesEndpoint",
  models: "api.modelsEndpoint",
};

/** The listener's three verdicts, named. */
const DISPOSITION_LABEL: Record<ApiAuthDisposition, TKey> = {
  required: "api.auth.required",
  accepted: "api.auth.accepted",
  rejected: "api.auth.rejected",
};

export function ApiKeysEndpointsPanel({
  endpoints,
  authMatrix,
}: {
  endpoints: ApiEndpointInfo;
  authMatrix: ApiAuthMatrixRow[];
}) {
  const t = useT();
  const titles = {
    panel: t("api.endpointsTitle"),
    base: t("api.baseUrl"),
    routes: t("api.availableEndpoints"),
    matrix: t("api.auth.matrixTitle"),
    rules: t("api.auth.serverRules"),
    endpoint: t("api.auth.endpoint"),
    footer: t("api.auth.matrixFooter"),
  };
  const columns = AUTH_MATRIX_COLUMNS.map(column => ({
    key: column.field,
    header: column.header,
  }));
  const rows = authMatrix.map(row => ({
    endpoint: row.endpoint,
    verdicts: AUTH_MATRIX_COLUMNS.map(column => t(DISPOSITION_LABEL[row[column.field]])),
  }));
  return (
    <div className="awi-endpoints">
      <section className="awi-section">
        <h3 className="awi-board-title">{titles.panel}</h3>
        <div className="awi-endpoint-base">
          <span className="muted small">{titles.base}</span>
          <EndpointLine url={endpoints.baseUrl} framed />
        </div>
      </section>
      <section className="awi-section">
        <h3 className="awi-board-title">{titles.routes}</h3>
        <ul className="awi-endpoint-list">
          {API_SURFACES.map(surface => (
            <li key={surface}>
              <EndpointLine label={t(ROUTE_LABEL[surface])} url={endpoints[surface]} />
            </li>
          ))}
        </ul>
      </section>
      <section className="awi-section api-auth-matrix-block">
        <div className="awi-section-head">
          <h3 className="awi-board-title">{titles.matrix}</h3>
          <span className="muted small">{titles.rules}</span>
        </div>
        <div className="api-auth-matrix-scroll">
          <table className="api-auth-matrix">
            <thead>
              <tr>
                <th>{titles.endpoint}</th>
                {columns.map(column => <th key={column.key}><code>{column.header}</code></th>)}
              </tr>
            </thead>
            <tbody>
              {rows.map(row => (
                <tr key={row.endpoint}>
                  <td><code>{row.endpoint}</code></td>
                  {row.verdicts.map((verdict, index) => (
                    <td key={columns[index]?.key ?? index}>{verdict}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="muted small awi-endpoint-matrix-foot">{titles.footer}</p>
      </section>
    </div>
  );
}

/** One route: its optional name, the URL, and the copy affordance for it. */
function EndpointLine({ label, url, framed = false }: { label?: string; url: string; framed?: boolean }) {
  return (
    <div className={framed ? "awi-endpoint-line awi-endpoint-line--framed" : "awi-endpoint-line"}>
      {label ? <span className="awi-endpoint-line-label">{label}</span> : null}
      <code className="awi-endpoint-line-url">{url}</code>
      <EndpointCopyIcon url={url} />
    </div>
  );
}

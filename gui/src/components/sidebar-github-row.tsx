/**
 * The sidebar's GitHub/update footer row.
 *
 * Star state is not decided here. `sidebar-github-star.ts` owns what the state is and what a
 * response means; `sidebar-github-row-state.ts` owns the interaction and the projection. This
 * file is what is left: the two polls, the click, and the markup.
 *
 * The update orb is always present, so "am I current?" is answerable at any time. It reads the
 * cached badge endpoint (no npm spawn per poll) purely to decide emphasis: an available update
 * gets the accent state plus a dot. Either way a click opens the repository's releases, because
 * Benes does not self-update in-process — updating is the package manager's job.
 */
import { useReducer, type ReactNode } from "react";
import { useKeyedClientResource } from "../client-resource";
import { readJsonIfOk } from "../fetch-json";
import { IconDownload, IconGithub, IconStar } from "../icons";
import { useT } from "../i18n/shared";
import { starClickMode, starOverrideFromResponse, type StarStatus, type UpdateBadge } from "./sidebar-github-star";
import {
  IDLE_STAR_INTERACTION,
  githubRowView,
  starInteraction,
  starOrbLabel,
  updateOrbLabel,
} from "./sidebar-github-row-state";

/** How often the row re-reads each endpoint, and where it reads it from. */
const STAR_POLL = { pollMs: 5 * 60_000 };
const BADGE_POLL = { pollMs: 10 * 60_000 };

/** Used until the endpoint reports which repository it belongs to. */
const FALLBACK_REPO_URL = "https://github.com/Wibias/Benes";

/**
 * The two management-API readings.
 *
 * Each one takes its api base rather than closing over it, so the reader is a real function
 * with a name instead of an anonymous thunk. The status gate and JSON decode come from
 * `fetch-json.ts`, so a non-JSON or empty 200 body is a `null` reading, not a throw.
 */
async function readStarStatus(apiBase: string, signal: AbortSignal): Promise<StarStatus | null> {
  const response = await fetch(`${apiBase}/api/github/star`, { signal });
  return (await readJsonIfOk<StarStatus>(response)) ?? null;
}

async function readUpdateBadge(apiBase: string, signal: AbortSignal): Promise<UpdateBadge | null> {
  const response = await fetch(`${apiBase}/api/update/badge`, { signal });
  return (await readJsonIfOk<UpdateBadge>(response)) ?? null;
}

/** One POST to the star endpoint, reduced to what the row has to do next. */
async function postStar(apiBase: string, basedOn: StarStatus["state"] | null) {
  const response = await fetch(`${apiBase}/api/github/star`, { method: "POST" });
  const body = response.ok ? await response.json() as StarStatus & { ok?: boolean } : null;
  return starOverrideFromResponse(body, basedOn ?? null);
}

/**
 * One orb in the row. Label and tooltip are the same string, so neither orb can end up as an
 * unlabelled circle; `modifier` is how the row shows state without a second component.
 */
function RowOrb({
  icon,
  label,
  modifier,
  disabled,
  pressed,
  badge,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  modifier?: string;
  disabled?: boolean;
  pressed?: boolean;
  badge?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      className={modifier === undefined ? "sidebar-orb" : `sidebar-orb ${modifier}`}
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      aria-pressed={pressed}
      title={label}
    >
      {icon}
      {badge === true && <span className="sidebar-orb-dot" aria-hidden="true" />}
    </button>
  );
}

/** What the row is given: the listener to read. */
interface SidebarGithubRowProps {
  apiBase: string;
}

export function SidebarGithubRow({ apiBase }: SidebarGithubRowProps) {
  const t = useT();
  const [interaction, dispatch] = useReducer(starInteraction, IDLE_STAR_INTERACTION);

  const starPoll = useKeyedClientResource(
    `sidebar-star:${apiBase}`,
    [apiBase],
    signal => readStarStatus(apiBase, signal),
    STAR_POLL,
  );
  const badgePoll = useKeyedClientResource(
    `sidebar-update-badge:${apiBase}`,
    [apiBase],
    signal => readUpdateBadge(apiBase, signal),
    BADGE_POLL,
  );

  const view = githubRowView({
    starStatus: starPoll.data ?? null,
    updateBadge: badgePoll.data ?? null,
    interaction,
    fallbackRepositoryUrl: FALLBACK_REPO_URL,
  });

  const openRepository = () => window.open(view.repositoryUrl, "_blank", "noopener,noreferrer");
  const openReleases = () => window.open(`${view.repositoryUrl}/releases`, "_blank", "noopener,noreferrer");

  const handleStar = async () => {
    const mode = starClickMode(view.starState, interaction.busy);
    if (mode === "ignore") return;
    if (mode === "open-repo") {
      openRepository();
      return;
    }
    dispatch({ kind: "request-started" });
    try {
      const decision = await postStar(apiBase, view.polledStarState);
      dispatch({ kind: "response-decided", override: decision.override });
      if (decision.openRepo) openRepository();
    } catch {
      // A `gh` failure mid-click must not leave a dead button: the repository page is the
      // honest fallback, because that is where the user can star it by hand.
      openRepository();
    } finally {
      dispatch({ kind: "request-settled" });
      starPoll.refresh();
    }
  };

  return (
    <div className="sidebar-github-row">
      <a className="sidebar-link sidebar-github-link" href={view.repositoryUrl} target="_blank" rel="noreferrer">
        <IconGithub /> {t("common.github")}
      </a>
      <div className="sidebar-github-actions">
        <RowOrb
          icon={<IconStar aria-hidden="true" fill={view.starred ? "currentColor" : undefined} />}
          label={starOrbLabel(view, t)}
          modifier={view.starred ? "sidebar-orb--starred" : undefined}
          disabled={view.starDisabled}
          pressed={view.starred}
          onClick={() => { void handleStar(); }}
        />
        <RowOrb
          icon={<IconDownload aria-hidden="true" />}
          label={updateOrbLabel(view, t)}
          modifier={view.updateAvailable ? "sidebar-orb--update" : undefined}
          badge={view.updateAvailable}
          onClick={openReleases}
        />
      </div>
    </div>
  );
}

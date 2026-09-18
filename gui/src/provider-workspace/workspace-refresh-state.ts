export type AuthoritativeWorkspaceRefreshFailure = {
  workspace: null;
  failed: true;
  discardCache: true;
};

export function authoritativeWorkspaceRefreshFailure(): AuthoritativeWorkspaceRefreshFailure {
  return { workspace: null, failed: true, discardCache: true };
}

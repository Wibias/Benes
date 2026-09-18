import assert from "node:assert/strict";
import test from "node:test";

test("workspace aggregate accepts Go's null hidden and recentEvents", async () => {
  const { parseProviderWorkspaceAggregate } = await import("../src/provider-workspace/workspace.ts");
  const parsed = parseProviderWorkspaceAggregate({
    summary: { totalProviders: 1, healthy: 1, attention: 0, disabled: 0, exposedModels: 0 },
    providers: [{
      id: "openai",
      connections: ["openai"],
      hidden: null,
      lifecycle: "healthy",
      modelCount: 0,
      access: { methods: [], defaultMethodId: "oauth", defaultAccess: true },
      disabled: false,
      lastValidated: null,
      downstream: { harnesses: null, routes: 0, subagents: 0 },
    }],
    attention: [],
    availability: {
      modelsAvailable: 0,
      modelsUnavailable: 0,
      staleProviderCatalogues: null,
      lastModelSync: null,
    },
    downstream: { harnessCount: 8, routeCount: 0, subAgentModelCount: 0, affectedRouteCount: 0 },
    recentEvents: null,
  });
  assert.equal(parsed?.providers[0]?.hidden.length, 0);
  assert.equal(parsed?.recentEvents.length, 0);
});

test("authoritative workspace refresh failure discards cached lifecycle truth", async () => {
  let state;
  try {
    state = await import("../src/provider-workspace/workspace-refresh-state.ts");
  } catch (error) {
    assert.fail(`workspace refresh state helper is missing: ${error}`);
  }

  assert.deepEqual(state.authoritativeWorkspaceRefreshFailure(), {
    workspace: null,
    failed: true,
    discardCache: true,
  });
});

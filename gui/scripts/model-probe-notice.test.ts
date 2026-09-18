import assert from "node:assert/strict";
import test from "node:test";

test("probeResultNotice confirms available and denies ChatGPT login probes", async () => {
  const { probeResultNotice, probeBulkNotice } = await import("../src/model-probe-notice.ts");
  const t = (key, vars = {}) => {
    if (key === "models.probeToastAvailable") return `${vars.model} is available`;
    if (key === "models.probeToastForward") return "ChatGPT login cannot be probed";
    if (key === "models.probeToastUnsupported") return "This provider cannot be probed";
    if (key === "models.probeToastAll") return `${vars.available} of ${vars.total} available`;
    if (key === "models.probeToastAllOk") return `All ${vars.total} available`;
    if (key === "models.probeFailed") return "Probe failed";
    return key;
  };

  assert.deepEqual(probeResultNotice(t, { model: "gpt-5.4", state: "available" }), {
    ok: true,
    text: "gpt-5.4 is available",
  });
  assert.deepEqual(probeResultNotice(t, { state: "unsupported_probe", reason: "forward_auth" }), {
    ok: false,
    text: "ChatGPT login cannot be probed",
  });
  assert.deepEqual(probeBulkNotice(t, [
    { state: "unsupported_probe", reason: "forward_auth" },
    { state: "unsupported_probe", reason: "forward_auth" },
  ]), {
    ok: false,
    text: "ChatGPT login cannot be probed",
  });
  assert.deepEqual(probeBulkNotice(t, [
    { state: "available" },
    { state: "unavailable" },
  ]), {
    ok: false,
    text: "1 of 2 available",
  });
});

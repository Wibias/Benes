"use strict";

function failure(reason) {
  return { ok: false, reason };
}

function timedOut(error) {
  const name = error?.name;
  return name === "AbortError" || name === "TimeoutError";
}

async function requestJsonCompletion({
  token,
  baseUrl,
  model,
  system,
  user,
  timeoutMs = 30_000,
}) {
  if (!token) return failure("missing_token");

  const root = String(baseUrl || "https://api.openai.com/v1").replace(/\/+$/, "");
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const response = await fetch(`${root}/chat/completions`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${token}`,
        "content-type": "application/json",
      },
      body: JSON.stringify({
        model: model || "gpt-4.1-mini",
        temperature: 0,
        response_format: { type: "json_object" },
        messages: [
          { role: "system", content: String(system || "") },
          { role: "user", content: String(user || "") },
        ],
      }),
      signal: controller.signal,
    });
    if (!response.ok) return failure(`http_${response.status}`);
    const payload = await response.json();
    const text = payload?.choices?.[0]?.message?.content;
    if (!text) return failure("empty_completion");
    return { ok: true, text };
  } catch (error) {
    return failure(timedOut(error) ? "timeout" : "network");
  } finally {
    clearTimeout(timer);
  }
}

module.exports = {
  requestJsonCompletion,
  completeJson: requestJsonCompletion,
};

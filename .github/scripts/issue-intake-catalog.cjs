"use strict";

// GitHub renders each field `label` as a `###` heading. This catalog is the
// alias list issue-quality uses. Live chooser labels belong here; older
// headings stay so already-filed issues still classify.

const KIND_PRIORITY = [
  "provider-compatibility",
  "documentation",
  "feature",
  "bug",
];

const KIND_TO_LABEL = {
  bug: "bug",
  feature: "enhancement",
  documentation: "documentation",
  "provider-compatibility": "provider-compatibility",
};

const LABEL_TO_KIND = [
  ["bug", "bug"],
  ["provider-compatibility", "provider-compatibility"],
  ["documentation", "documentation"],
  ["enhancement", "feature"],
];

const FEATURE = {
  current: [
    "Job you need done",
    "Why current Benes cannot do this",
    "Desired result and interface",
    "One concrete interaction",
  ],
  legacyPair: ["Problem to solve", "Proposed solution"],
  goal: [
    "Job you need done",
    "What workflow do you want?",
    "What are you trying to accomplish?",
    "Goal / Problem",
    "Goal/Problem",
    "Problem to solve",
  ],
  limitation: [
    "Why current Benes cannot do this",
    "What is missing or blocked in Benes now?",
    "What prevents this today?",
    "Current limitation",
    "Current workaround",
  ],
  behaviour: [
    "Desired result and interface",
    "What should the listener or CLI do?",
    "What should Benes do?",
    "Expected behaviour",
    "Expected behavior",
    "Proposed solution",
  ],
  example: [
    "One concrete interaction",
    "Example command, config, or request",
    "Example usage or interface",
    "Example usage",
    "Example",
  ],
  detectAliases: [
    "Job you need done",
    "Why current Benes cannot do this",
    "Desired result and interface",
    "One concrete interaction",
    "What workflow do you want?",
    "What is missing or blocked in Benes now?",
    "What should the listener or CLI do?",
    "Example command, config, or request",
    "What are you trying to accomplish?",
    "What prevents this today?",
    "What should Benes do?",
    "Example usage or interface",
    "Goal / Problem",
    "Goal/Problem",
    "Expected behaviour",
    "Expected behavior",
    "Current limitation",
    "Current workaround",
    "Example usage",
  ],
};

const BUG = {
  current: [
    "Client that hit the failure",
    "What happened",
    "Commands that reproduce it",
  ],
  aliases: [
    "Which client showed this?",
    "Client or integration",
    "What failed?",
    "Summary",
    "How to reproduce",
    "Reproduction",
  ],
  legacyPair: ["Summary", "Reproduction"],
  client: [
    "Client that hit the failure",
    "Which client showed this?",
    "Client or integration",
  ],
  failure: ["What happened", "What failed?", "Summary"],
  reproduction: ["Commands that reproduce it", "How to reproduce", "Reproduction"],
  version: ["benes version or commit", "Version", "Benes version"],
  os: ["Host OS", "Operating system", "OS"],
  evidenceAliases: [
    "Redacted log or HTTP trace",
    "Commands that reproduce it",
    "Steps to reproduce",
    "How to reproduce",
    "What fails / what passes",
    "Debug evidence (benes debug provider)",
    "Debug evidence",
    "Logs or error text",
    "Logs or error output",
    "Error output",
    "Stack trace",
  ],
  envVersionKeys: ["Benes", "Benes version"],
  envOsKeys: ["OS", "Operating system", "Host OS"],
};

const PROVIDER = {
  current: [
    "Upstream provider",
    "Listener path or capability",
    "What the listener returned",
    "Correct behaviour per spec or client",
  ],
  aliases: [
    "Provider or upstream service",
    "Endpoint or capability",
    "Current behaviour",
    "Expected behaviour",
    "What Benes returns now",
    "What the client or spec requires",
    "Minimal redacted request or reproduction",
  ],
  providerName: ["Upstream provider", "Provider or upstream service"],
  endpoint: ["Listener path or capability", "Endpoint or capability"],
  benesVersion: ["benes version or commit", "Benes version"],
  currentBehaviour: [
    "What the listener returned",
    "What Benes returns now",
    "Current behaviour",
  ],
  expectedBehaviour: [
    "Correct behaviour per spec or client",
    "What the client or spec requires",
    "Expected behaviour",
    "Expected behavior",
  ],
  request: [
    "Smallest redacted request",
    "Redacted request that shows the mismatch",
    "Minimal redacted request or reproduction",
  ],
  response: [
    "What the listener returned",
    "HTTP status and redacted body",
    "Actual response or error",
  ],
  upstreamDocs: ["Upstream spec", "Upstream documentation"],
};

const DOCS = {
  current: [
    "Page or file",
    "What is wrong",
    "What it should say",
  ],
  aliases: [
    "Documentation problem type",
    "Documentation location",
    "What is inaccurate or missing?",
    "What is wrong or missing?",
  ],
  location: ["Page or file", "Documentation location"],
  problem: [
    "What is wrong",
    "What is inaccurate or missing?",
    "What is wrong or missing?",
  ],
  expected: [
    "What it should say",
    "What should the page say?",
    "What should the documentation explain instead?",
  ],
};

const NEAR_MISS_BUG_HEADINGS = [
  "Description",
  "Steps to reproduce",
  "How to reproduce",
  "Commands that reproduce it",
  "Log",
  "Logs",
  "Log entry",
  "Error",
  "Error output",
  "Stack trace",
];

const AREA_FIELD_HEADINGS = [
  "Which Benes surface failed",
  "Likely landing place",
  "Area",
];

const AREA_DROPDOWN_TO_LABEL = {
  cli: ["cli"],
  "loopback proxy": ["proxy"],
  "proxy and routing": ["proxy"],
  dashboard: ["gui"],
  "provider adapter": ["provider"],
  "provider adapters": ["provider"],
  "credentials and oauth": ["account-pool"],
  "authentication and account pool": ["account-pool"],
  "model catalog": ["catalog"],
  "catalog / models": ["catalog"],
  streaming: ["streaming"],
  "tools / mcp / web search": ["tools"],
  "install and packaging": ["install"],
  "installation or packaging": ["install"],
  "service lifecycle": ["service"],
  "service lifecycle (config injection)": ["service"],
  "platform (windows / macos / linux)": ["platform"],
  documentation: [],
  docs: [],
  "multiple areas": [],
  unsure: [],
  other: [],
};

const AREA_LABEL_META = {
  provider: {
    color: "1D76DB",
    description: "Provider adapters, presets, and upstream wire mismatches",
  },
  "account-pool": {
    color: "5319E7",
    description: "OAuth, stored accounts, quota windows, and ChatGPT pool hops",
  },
  catalog: {
    color: "006B75",
    description: "Model catalog, visibility, slugs, and routed entries",
  },
  gui: {
    color: "D93F0B",
    description: "Dashboard boards and tray UI",
  },
  cli: {
    color: "FBCA04",
    description: "Go CLI, config inject, and packaging flags",
  },
  proxy: {
    color: "0E8A16",
    description: "Loopback listener, routing, and management API",
  },
  platform: {
    color: "BFDADC",
    description: "OS, service manager, ACL, and tray host",
  },
  streaming: {
    color: "C5DEF5",
    description: "HTTP/SSE streams and terminal frames",
  },
  tools: {
    color: "F9D0C4",
    description: "tool_calls, MCP, and web-search sidecars",
  },
  install: {
    color: "EDEDED",
    description: "Installation or packaging",
  },
  service: {
    color: "EDEDED",
    description: "Service lifecycle (WinSW, launchd, scheduler)",
  },
};

const AREA_NARRATIVE_HEADINGS = [
  "What happened",
  "What should have happened",
  "Commands that reproduce it",
  "What failed?",
  "How to reproduce",
  "Summary",
  "Reproduction",
  "Job you need done",
  "Why current Benes cannot do this",
  "Desired result and interface",
  "One concrete interaction",
  "What workflow do you want?",
  "What is missing or blocked in Benes now?",
  "What should the listener or CLI do?",
  "Example command, config, or request",
  "What are you trying to accomplish?",
  "What prevents this today?",
  "What should Benes do?",
  "Example usage or interface",
  "What the listener returned",
  "Correct behaviour per spec or client",
  "What the client or spec requires",
  "Smallest redacted request",
  "What Benes returns now",
  "Redacted request that shows the mismatch",
  "Current behaviour",
  "Expected behaviour",
  "Minimal redacted request or reproduction",
  "What is wrong",
  "What it should say",
  "What is inaccurate or missing?",
  "What is wrong or missing?",
  "Page or file",
  "Documentation location",
];

const ISSUE_FORMS = [
  {
    file: "bug_report.yml",
    chooserName: "Bug report",
    kind: "bug",
    shippedLabels: ["bug"],
  },
  {
    file: "feature_request.yml",
    chooserName: "Feature request",
    kind: "feature",
    shippedLabels: ["enhancement"],
  },
  {
    file: "documentation.yml",
    chooserName: "Documentation",
    kind: "documentation",
    shippedLabels: ["documentation"],
  },
  {
    file: "provider_compatibility.yml",
    chooserName: "Provider or API compatibility",
    kind: "provider-compatibility",
    shippedLabels: ["provider-compatibility", "provider"],
  },
];

const TITLE_FEATURE_PREFIX = "[feature]:";
const TITLE_BUG_PREFIX = "[bug]:";

module.exports = {
  KIND_PRIORITY,
  KIND_TO_LABEL,
  LABEL_TO_KIND,
  FEATURE,
  BUG,
  PROVIDER,
  DOCS,
  NEAR_MISS_BUG_HEADINGS,
  AREA_FIELD_HEADINGS,
  AREA_DROPDOWN_TO_LABEL,
  AREA_LABEL_META,
  AREA_NARRATIVE_HEADINGS,
  ISSUE_FORMS,
  TITLE_FEATURE_PREFIX,
  TITLE_BUG_PREFIX,
};

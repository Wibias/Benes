"use strict";

const fs = require("node:fs");
const path = require("node:path");

const GITHUB_FIELD_TYPES = new Set([
  "markdown",
  "textarea",
  "input",
  "dropdown",
  "checkboxes",
]);

const REQUIRED_TEMPLATE_FILES = [
  "bug_report.yml",
  "feature_request.yml",
  "provider_compatibility.yml",
  "documentation.yml",
  "config.yml",
];

const CREDENTIAL_ACK =
  /token|api keys?|cookies?|authorization|secret|credential|personal data/i;

const FORM_CONTRACTS = {
  "bug_report.yml": {
    labels: ["bug"],
    credentialAck: true,
    slots: [
      { id: "product-surface", type: "dropdown", required: true },
      { id: "talking-client", type: "dropdown", required: true },
      { id: "happened", type: "textarea", required: true },
      { id: "expected-result", type: "textarea", required: true },
      { id: "rerun", type: "textarea", required: true },
      { id: "benes-build", type: "input", required: true },
      { id: "host-os", type: "input", required: true },
    ],
  },
  "feature_request.yml": {
    labels: ["enhancement"],
    credentialAck: true,
    slots: [
      { id: "job", type: "textarea", required: true },
      { id: "blocked-by", type: "textarea", required: true },
      { id: "ship-shape", type: "textarea", required: true },
      { id: "concrete-interaction", type: "textarea", required: true },
      { id: "landing", type: "dropdown", required: true },
    ],
  },
  "provider_compatibility.yml": {
    labels: ["provider-compatibility", "provider"],
    credentialAck: true,
    slots: [
      { id: "vendor", type: "input", required: true },
      { id: "listener-path", type: "input", required: true },
      { id: "talking-client", type: "dropdown", required: true },
      { id: "benes-build", type: "input", required: true },
      { id: "spec-says", type: "textarea", required: true },
      { id: "listener-returned", type: "textarea", required: true },
      { id: "smallest-request", type: "textarea", required: true },
      { id: "spec-url", type: "input", required: true },
    ],
  },
  "documentation.yml": {
    labels: ["documentation"],
    credentialAck: true,
    slots: [
      { id: "doc-path", type: "input", required: true },
      { id: "inaccuracy", type: "textarea", required: true },
      { id: "accurate", type: "textarea", required: true },
    ],
  },
};

function lineIndent(line) {
  let n = 0;
  while (n < line.length && line[n] === " ") n += 1;
  if (n < line.length && line[n] === "\t") {
    throw new Error("issue-form YAML must use spaces, not tabs");
  }
  return n;
}

function unquote(value) {
  if (
    (value.startsWith('"') && value.endsWith('"') && value.length >= 2) ||
    (value.startsWith("'") && value.endsWith("'") && value.length >= 2)
  ) {
    return value.slice(1, -1);
  }
  return value;
}

function afterColon(line) {
  const colon = line.indexOf(":");
  if (colon === -1) return "";
  return unquote(line.slice(colon + 1).trim());
}

function isCommentOrEmpty(line) {
  const trimmed = line.trim();
  return trimmed === "" || trimmed.startsWith("#");
}

function isBlockScalar(line) {
  const value = afterColon(line);
  return value === "|" || value === ">" || value.startsWith("|") || value.startsWith(">");
}

function skipBlock(lines, start, baseIndent) {
  let i = start + 1;
  while (i < lines.length) {
    if (isCommentOrEmpty(lines[i])) {
      i += 1;
      continue;
    }
    if (lineIndent(lines[i]) <= baseIndent) break;
    i += 1;
  }
  return i;
}

function newField(type) {
  return {
    type,
    id: null,
    label: null,
    required: false,
    options: [],
    checkboxOptions: [],
  };
}

function extractIssueForm(raw) {
  const lines = String(raw).replace(/^\uFEFF/, "").split(/\r?\n/);
  const doc = {
    name: "",
    description: "",
    labels: [],
    body: [],
    blankIssuesEnabled: null,
    contactLinks: [],
  };

  let section = "root";
  let field = null;
  let inOptions = false;
  let checkbox = null;
  let contact = null;
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];
    if (isCommentOrEmpty(line)) {
      i += 1;
      continue;
    }
    const indent = lineIndent(line);
    const trimmed = line.trim();

    if (isBlockScalar(line)) {
      i = skipBlock(lines, i, indent);
      continue;
    }

    if (indent === 0) {
      section = "root";
      field = null;
      inOptions = false;
      checkbox = null;
      contact = null;
      if (trimmed.startsWith("name:")) doc.name = afterColon(line);
      else if (trimmed.startsWith("description:")) doc.description = afterColon(line);
      else if (trimmed.startsWith("blank_issues_enabled:")) {
        doc.blankIssuesEnabled = afterColon(line) === "false" ? false : afterColon(line) === "true";
      } else if (trimmed === "labels:" || trimmed.startsWith("labels:")) {
        section = "labels";
        const inline = afterColon(line);
        if (inline.startsWith("[") && inline.endsWith("]")) {
          doc.labels.push(
            ...inline
              .slice(1, -1)
              .split(",")
              .map((item) => unquote(item.trim()))
              .filter(Boolean),
          );
        }
      } else if (trimmed === "body:") section = "body";
      else if (trimmed === "contact_links:") section = "contact";
      i += 1;
      continue;
    }

    if (section === "labels" && indent === 2 && trimmed.startsWith("- ")) {
      doc.labels.push(unquote(trimmed.slice(2).trim()));
      i += 1;
      continue;
    }

    if (section === "contact" && indent === 2 && trimmed.startsWith("- ")) {
      contact = { name: "", url: "", about: "" };
      doc.contactLinks.push(contact);
      if (trimmed.startsWith("- name:")) contact.name = afterColon(trimmed.slice(2));
      else if (trimmed.startsWith("- url:")) contact.url = afterColon(trimmed.slice(2));
      i += 1;
      continue;
    }
    if (section === "contact" && contact && indent === 4) {
      if (trimmed.startsWith("name:")) contact.name = afterColon(line);
      else if (trimmed.startsWith("url:")) contact.url = afterColon(line);
      else if (trimmed.startsWith("about:")) contact.about = afterColon(line);
      i += 1;
      continue;
    }

    if (section === "body" && indent === 2 && trimmed.startsWith("- type:")) {
      field = newField(afterColon(trimmed.slice(2)));
      doc.body.push(field);
      inOptions = false;
      checkbox = null;
      i += 1;
      continue;
    }
    if (!field || section !== "body") {
      i += 1;
      continue;
    }

    if (indent === 4 && trimmed.startsWith("id:")) {
      field.id = afterColon(line);
      inOptions = false;
    } else if (indent === 4 && trimmed === "attributes:") {
      inOptions = false;
    } else if (indent === 4 && trimmed === "validations:") {
      inOptions = false;
    } else if (indent === 6 && trimmed.startsWith("label:")) {
      field.label = afterColon(line);
    } else if (indent === 6 && trimmed === "options:") {
      inOptions = true;
    } else if (indent === 6 && trimmed.startsWith("required:")) {
      field.required = afterColon(line) === "true";
    } else if (inOptions && indent === 8 && trimmed.startsWith("- label:")) {
      checkbox = { label: afterColon(trimmed.slice(2)), required: false };
      field.checkboxOptions.push(checkbox);
    } else if (inOptions && indent === 8 && trimmed.startsWith("- ")) {
      checkbox = null;
      field.options.push(unquote(trimmed.slice(2).trim()));
    } else if (checkbox && indent === 10 && trimmed.startsWith("required:")) {
      checkbox.required = afterColon(line) === "true";
    }

    i += 1;
  }

  return doc;
}

function credentialAckErrors(elements) {
  for (const el of elements) {
    if (el.type !== "checkboxes") continue;
    const hit = el.checkboxOptions.find(
      (opt) => opt.required && CREDENTIAL_ACK.test(String(opt.label || "")),
    );
    if (hit) return [];
  }
  return ["missing required credential-redaction acknowledgement"];
}

function formErrors(file, doc) {
  const errors = [];
  const base = path.basename(file);
  if (!doc.name.trim()) errors.push("form is missing a name");
  if (!doc.description.trim()) errors.push("form is missing a description");
  if (doc.body.length === 0) errors.push("form is missing a body");

  const types = [];
  for (const el of doc.body) {
    if (!el.type) {
      errors.push("body element is missing a type");
      continue;
    }
    types.push(el.type);
    if (!GITHUB_FIELD_TYPES.has(el.type)) {
      errors.push(`unsupported field type '${el.type}'`);
    }
  }

  const ids = doc.body.map((el) => el.id).filter(Boolean);
  const dupes = ids.filter((id, index) => ids.indexOf(id) !== index);
  if (dupes.length) {
    errors.push(`duplicate field ids: ${[...new Set(dupes)].join(", ")}`);
  }

  const contract = FORM_CONTRACTS[base];
  if (contract) {
    for (const wanted of contract.labels) {
      if (!doc.labels.includes(wanted)) {
        errors.push(`form must apply the '${wanted}' label`);
      }
    }
    const byId = new Map(doc.body.filter((el) => el.id).map((el) => [el.id, el]));
    for (const slot of contract.slots) {
      const el = byId.get(slot.id);
      if (!el) {
        errors.push(`missing required field '${slot.id}'`);
        continue;
      }
      if (el.type !== slot.type) {
        errors.push(`field '${slot.id}' must be a ${slot.type}`);
      }
      if (slot.required && !el.required) {
        errors.push(`field '${slot.id}' must be required`);
      }
    }
    if (contract.credentialAck) errors.push(...credentialAckErrors(doc.body));
  }

  return { file: base, types: types.length, ids: ids.length, errors, doc };
}

function configErrors(raw, doc) {
  const errors = [];
  if (doc.blankIssuesEnabled !== false) {
    errors.push("config.yml must set blank_issues_enabled to false");
  }
  const advisory = doc.contactLinks.some(
    (link) =>
      typeof link.url === "string" &&
      link.url.includes("github.com/Wibias/Benes/security/advisories"),
  );
  if (!advisory) {
    errors.push("config.yml must link the private vulnerability advisory form");
  }
  if (!/blank_issues_enabled:\s*false/.test(raw)) {
    errors.push("config.yml must set blank_issues_enabled to false");
  }
  return [...new Set(errors)];
}

function loadDir(dir) {
  return fs.readdirSync(dir).filter((name) => name.endsWith(".yml")).sort();
}

function inspectTemplateDir(dir) {
  const names = loadDir(dir);
  const missing = REQUIRED_TEMPLATE_FILES.filter((name) => !names.includes(name));
  const forms = [];
  const config = { errors: [] };
  if (missing.length) config.errors.push(`missing template files: ${missing.join(", ")}`);

  for (const name of names) {
    const file = path.join(dir, name);
    const raw = fs.readFileSync(file, "utf8");
    let doc;
    try {
      doc = extractIssueForm(raw);
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      if (name === "config.yml") config.errors.push(`config.yml parse failed: ${message}`);
      else forms.push({ file: name, types: 0, ids: 0, errors: [`form extract failed: ${message}`] });
      continue;
    }
    if (name === "config.yml") {
      config.errors.push(...configErrors(raw, doc));
      continue;
    }
    forms.push(formErrors(name, doc));
  }
  return { forms, config };
}

function validateIssueForm(file, raw) {
  try {
    return formErrors(file, extractIssueForm(raw));
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return {
      file: path.basename(file),
      types: 0,
      ids: 0,
      errors: [`form extract failed: ${message}`],
    };
  }
}

function validateIssueForms(dir) {
  return inspectTemplateDir(dir).forms;
}

function validateConfigYml(dir) {
  const file = path.join(dir, "config.yml");
  if (!fs.existsSync(file)) return ["config.yml is missing"];
  const raw = fs.readFileSync(file, "utf8");
  try {
    return configErrors(raw, extractIssueForm(raw));
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    return [`config.yml parse failed: ${message}`];
  }
}

function main(dir = path.join(__dirname, "..", "ISSUE_TEMPLATE")) {
  const { forms, config } = inspectTemplateDir(dir);
  let ok = true;
  for (const result of forms) {
    if (result.errors.length) {
      ok = false;
      for (const error of result.errors) console.error(`${result.file}: ${error}`);
      continue;
    }
    console.log(`${result.file}: ${result.types} fields, ${result.ids} ids`);
  }
  for (const error of config.errors) {
    ok = false;
    console.error(error);
  }
  if (!ok) process.exit(1);
  console.log("Issue templates satisfy the Benes intake contract.");
}

module.exports = {
  GITHUB_FIELD_TYPES,
  FORM_CONTRACTS,
  extractIssueForm,
  validateIssueForm,
  validateIssueForms,
  validateConfigYml,
  inspectTemplateDir,
  main,
};

if (require.main === module) {
  main();
}

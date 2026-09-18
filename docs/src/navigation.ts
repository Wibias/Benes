/**
 * Navigation for the public site.
 *
 * One list drives both the Starlight sidebar (astro.config.mjs) and the header
 * dropdowns (src/components/Header.astro), so a page can no longer be listed in
 * one surface and missing from the other.
 */

export type NavLink = { readonly label: string; readonly slug: string };
export type NavGroup = { readonly label: string; readonly links: readonly NavLink[] };

export const siteOrigin = "https://wibias.github.io";
export const siteBase = "/Benes/";
export const siteUrl = (siteOrigin + siteBase).replace(/\/+$/, "");
export const repositoryUrl = "https://github.com/Wibias/Benes";
export const packageUrl = "https://www.npmjs.com/package/benes";

export const navGroups: readonly NavGroup[] = [
  {
    label: "Start",
    links: [
      { label: "Install", slug: "start/install" },
      { label: "First run", slug: "start/first-run" },
      { label: "How traffic flows", slug: "start/how-traffic-flows" },
      { label: "For agents", slug: "start/agents" },
    ],
  },
  {
    label: "Use",
    links: [
      { label: "Dashboard", slug: "use/dashboard" },
      { label: "Providers", slug: "use/providers" },
      { label: "Model ids", slug: "use/model-ids" },
      { label: "Codex", slug: "use/codex" },
      { label: "Other clients", slug: "use/clients" },
      { label: "Combos", slug: "use/combos" },
      { label: "Sidecars", slug: "use/sidecars" },
      { label: "Sub-agents", slug: "use/sub-agents" },
    ],
  },
  {
    label: "Reference",
    links: [
      { label: "CLI", slug: "reference/cli" },
      { label: "Config", slug: "reference/config" },
      { label: "HTTP", slug: "reference/http" },
      { label: "Usage", slug: "reference/usage" },
      { label: "Storage", slug: "reference/storage" },
      { label: "Sessions", slug: "reference/sessions" },
      { label: "Diagnostics", slug: "reference/diagnostics" },
      { label: "Packages", slug: "reference/packages" },
    ],
  },
  {
    label: "Project",
    links: [
      { label: "Contributing", slug: "contributing" },
      { label: "PR contract", slug: "contributing/pr-quality" },
      { label: "Troubleshooting", slug: "troubleshooting" },
    ],
  },
];

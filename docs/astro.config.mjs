// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

import { navGroups, siteBase, siteOrigin, siteUrl } from "./src/navigation.ts";

export default defineConfig({
  site: siteOrigin,
  base: siteBase,
  trailingSlash: "ignore",
  vite: { build: { cssMinify: "esbuild" } },
  integrations: [
    starlight({
      title: "benes",
      description:
        "Local Go proxy. Codex, Claude Code, and other clients keep their own interfaces and send their requests to one loopback listener on 127.0.0.1:23100.",
      tagline: "One loopback port in front of the providers you configure.",
      logo: {
        light: "./src/assets/logo-light.png",
        dark: "./src/assets/logo-dark.png",
        alt: "benes",
        /* The artwork already carries the wordmark, so Starlight prints no title text. */
        replacesTitle: true,
      },
      favicon: "/favicon.ico",
      customCss: [
        "@fontsource-variable/geist",
        "pretendard/dist/web/variable/pretendardvariable-dynamic-subset.css",
        "./src/styles/custom.css",
      ],
      components: {
        Header: "./src/components/Header.astro",
        PageTitle: "./src/components/PageTitle.astro",
      },
      head: [
        { tag: "meta", attrs: { property: "og:type", content: "website" } },
        { tag: "meta", attrs: { property: "og:image", content: siteUrl + "/og.png" } },
        { tag: "meta", attrs: { name: "twitter:card", content: "summary_large_image" } },
        { tag: "meta", attrs: { name: "twitter:image", content: siteUrl + "/og.png" } },
        { tag: "meta", attrs: { name: "theme-color", media: "(prefers-color-scheme: light)", content: "#f7f7f5" } },
        { tag: "meta", attrs: { name: "theme-color", media: "(prefers-color-scheme: dark)", content: "#0d1117" } },
      ],
      social: [{ icon: "github", label: "GitHub", href: "https://github.com/Wibias/Benes" }],
      editLink: { baseUrl: "https://github.com/Wibias/Benes/edit/main/docs/" },
      lastUpdated: true,
      defaultLocale: "root",
      locales: { root: { label: "English", lang: "en" } },
      sidebar: navGroups.map((group) => ({
        label: group.label,
        items: group.links.map((link) => ({ label: link.label, slug: link.slug })),
      })),
    }),
  ],
});

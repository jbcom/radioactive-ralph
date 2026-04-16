// @ts-check
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

// Archived Astro/Starlight prototype for the old site-local docs experiment.
// The live docs now build from repo-root docs/ with Sphinx.

export default defineConfig({
  site: "https://jonbogaty.com",
  base: "/radioactive-ralph",
  integrations: [
    starlight({
      title: "radioactive-ralph",
      description:
        "Archived Astro prototype for radioactive-ralph docs.",
      logo: {
        src: "./src/assets/ralph-mascot.png",
        alt: "Radioactive Ralph mascot",
      },
      favicon: "/favicon.svg",
      social: [
        {
          icon: "github",
          label: "GitHub",
          href: "https://github.com/jbcom/radioactive-ralph",
        },
      ],
      customCss: ["./src/styles/ralph.css"],
      components: {
        // Full-bespoke hero for the landing to carry Ralph's personality.
        Hero: "./src/components/RalphHero.astro",
      },
      lastUpdated: false,
      pagefind: true,
      sidebar: [
        {
          label: "Getting Started",
          autogenerate: { directory: "getting-started" },
        },
        {
          label: "Guides",
          autogenerate: { directory: "guides" },
        },
        {
          label: "Variants",
          autogenerate: { directory: "variants" },
        },
        {
          label: "Reference",
          autogenerate: { directory: "reference" },
        },
        {
          label: "Design",
          autogenerate: { directory: "design" },
          collapsed: true,
        },
        {
          label: "API (Go)",
          autogenerate: { directory: "api" },
          collapsed: true,
        },
      ],
    }),
  ],
});

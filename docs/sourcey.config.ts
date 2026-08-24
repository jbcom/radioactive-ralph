import { defineConfig, godoc, markdown } from "sourcey";

export default defineConfig({
  name: "radioactive-ralph",
  siteUrl: "https://jonbogaty.com",
  baseUrl: "/radioactive-ralph",
  prettyUrls: "slash",
  theme: {
    preset: "default",
    colors: {
      primary: "#22c55e",
      light: "#65e58c",
      dark: "#166534",
    },
    fonts: {
      sans: '"Schoolbell", cursive',
      mono: '"SFMono-Regular", Consolas, "Liberation Mono", monospace',
    },
    layout: {
      sidebar: "18rem",
      toc: "17rem",
      content: "48rem",
    },
    css: ["./_static/custom.css"],
  },
  logo: {
    light: "./_static/radioactive-ralph-mark.svg",
    dark: "./_static/radioactive-ralph-mark.svg",
    href: "/",
  },
  favicon: "./_static/radioactive-ralph-mark.svg",
  ogImage: "./_static/ralph-mascot.png",
  repo: "https://github.com/jbcom/radioactive-ralph",
  editBranch: "main",
  editBasePath: "docs",
  navbar: {
    links: [
      { type: "link", label: "GitHub", href: "https://github.com/jbcom/radioactive-ralph" },
      { type: "link", label: "Releases", href: "https://github.com/jbcom/radioactive-ralph/releases" },
    ],
    primary: { type: "button", label: "Get started", href: "/getting-started/" },
  },
  footer: {
    links: [
      { type: "github", href: "https://github.com/jbcom/radioactive-ralph" },
      { type: "link", label: "Security", href: "https://github.com/jbcom/radioactive-ralph/security/policy" },
    ],
  },
  search: { featured: ["", "getting-started/", "guides/plan-format/", "reference/architecture/"] },
  navigation: {
    tabs: [
      {
        tab: "Documentation",
        slug: "",
        source: markdown({
          groups: [
            {
              group: "Getting Started",
              pages: ["index", "getting-started/index"],
            },
            {
              group: "Guides",
              pages: [
                "guides/index",
                "guides/plan-format",
                "guides/launch",
                "guides/tui",
                "guides/gui",
                "guides/safety-floors",
                "guides/self-test",
                "guides/cassette-vcr",
                "guides/transports",
                "guides/design",
                "guides/demo",
              ],
            },
            {
              group: "Runbooks",
              pages: [
                "runbooks/index",
                "runbooks/install-first-run",
                "runbooks/provider-auth",
                "runbooks/service",
                "runbooks/platforms",
                "runbooks/troubleshooting",
              ],
            },
            {
              group: "Reference",
              pages: [
                "reference/index",
                "reference/architecture",
                "reference/state",
                "reference/testing",
                "reference/documentation",
              ],
            },
            {
              group: "Release",
              pages: ["launch/index", "launch/release-checklist"],
            },
          ],
        }),
      },
      {
        tab: "Architecture",
        source: markdown({
          groups: [
            {
              group: "Design notes",
              pages: [
                "design/index",
                "design/deterministic-execution",
                "design/completion-and-a2a",
                "design/config-layers",
                "design/declarative-provider-bindings",
                "design/enforcement-adapters",
                "design/enforcement-policy",
                "design/exact-provider-identity",
                "design/plan-adaptive-concurrency",
                "design/provider-contract",
                "design/provider-write-containment",
              ],
            },
            {
              group: "Authoritative specifications",
              pages: [
                "superpowers/PILLARS",
                "superpowers/specs/2026-07-16-supervisor-architecture-design",
                "superpowers/specs/2026-07-16-a2a-plan-lib-evaluation",
                "superpowers/specs/2026-07-16-substrate-evaluation",
                "superpowers/specs/2026-07-17-async-dispatch-never-block-design",
                "superpowers/specs/2026-07-17-attach-event-stream-design",
                "superpowers/specs/2026-07-17-attach-live-consumers-design",
                "superpowers/specs/2026-07-17-events-cli-design",
                "superpowers/specs/2026-07-17-fyne-gui-client-design",
                "superpowers/specs/2026-07-17-guided-first-run-onboarding-design",
                "superpowers/specs/2026-07-17-ipc-drive-api-design",
                "superpowers/specs/2026-07-17-native-packaging-design",
                "superpowers/specs/2026-07-17-tui-macro-live-events-design",
                "superpowers/specs/2026-07-26-dag-integration-design",
                "superpowers/specs/2026-07-26-windows-scm-safety-disable-design",
                "superpowers/specs/2026-08-20-windows-wsl2-dispatch-design",
              ],
            },
          ],
        }),
      },
      {
        tab: "Go API",
        source: godoc({
          module: "..",
          // Ralph is a CLI application, not an importable library. Its
          // internal packages are documented by the architecture material;
          // only the command package is part of the supported public surface.
          packages: ["./cmd/..."],
          mode: "live",
          includeTests: true,
        }),
      },
    ],
  },
});

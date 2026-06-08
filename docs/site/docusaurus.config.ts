import {themes as prismThemes} from 'prism-react-renderer';
import type {Config} from '@docusaurus/types';
import type * as Preset from '@docusaurus/preset-classic';

const config: Config = {
  title: 'OpenUsage',
  tagline: 'Local-first terminal dashboard for AI tool spend and quotas',
  favicon: 'img/favicon.svg',

  future: {
    v4: true,
  },

  url: 'https://openusage.sh',
  // Preview builds (Cloudflare Pages *.pages.dev) serve at the host root,
  // so baseUrl needs to be "/". Production builds are mounted at openusage.sh/docs/
  // by the website-deploy workflow.
  baseUrl: process.env.DOCS_PREVIEW === '1' ? '/' : '/docs/',
  trailingSlash: true,

  organizationName: 'janekbaraniewski',
  projectName: 'openusage',

  onBrokenLinks: 'warn',

  markdown: {
    mermaid: true,
    hooks: {
      onBrokenMarkdownLinks: 'warn',
    },
  },

  i18n: {
    defaultLocale: 'en',
    locales: ['en'],
  },

  clientModules: [require.resolve('./src/clientModules/posthog.ts')],

  customFields: {
    posthogKey: process.env.POSTHOG_KEY ?? '',
    posthogHost: process.env.POSTHOG_HOST ?? '',
  },

  // Site-level structured data (JSON-LD), injected into every docs page.
  // Uses the same @id URIs as the marketing site (openusage.sh) so search and
  // answer engines consolidate both into one entity. Structured data is a
  // measured AEO signal: pages that pair clean structure with schema earn
  // materially more AI citations. Per-page schema (FAQPage, etc.) is added in
  // the relevant page via @docusaurus/Head; BreadcrumbList is emitted
  // automatically by Docusaurus.
  headTags: [
    {
      tagName: 'script',
      attributes: {type: 'application/ld+json'},
      innerHTML: JSON.stringify({
        '@context': 'https://schema.org',
        '@graph': [
          {
            '@type': 'Organization',
            '@id': 'https://openusage.sh/#organization',
            name: 'OpenUsage',
            alternateName: 'OpenUsage.sh',
            url: 'https://openusage.sh/',
            logo: {
              '@type': 'ImageObject',
              url: 'https://openusage.sh/brand/og.png',
            },
            description:
              'OpenUsage.sh is an open-source, local-first terminal dashboard for tracking spend, quotas, and rate limits across AI coding tools.',
            founder: {
              '@type': 'Person',
              name: 'Jan Baraniewski',
              url: 'https://baraniewski.com',
            },
            sameAs: ['https://github.com/janekbaraniewski/openusage'],
          },
          {
            '@type': 'WebSite',
            '@id': 'https://openusage.sh/#website',
            name: 'OpenUsage.sh',
            url: 'https://openusage.sh/',
            publisher: {'@id': 'https://openusage.sh/#organization'},
            inLanguage: 'en',
          },
          {
            '@type': 'SoftwareApplication',
            '@id': 'https://openusage.sh/#software',
            name: 'OpenUsage',
            alternateName: 'OpenUsage.sh',
            applicationCategory: 'DeveloperApplication',
            applicationSubCategory:
              'Quota tracker and usage dashboard for coding agents',
            operatingSystem: 'macOS, Linux, Windows',
            isAccessibleForFree: true,
            publisher: {'@id': 'https://openusage.sh/#organization'},
            isPartOf: {'@id': 'https://openusage.sh/#website'},
            description:
              'OpenUsage is an open-source terminal dashboard that tracks spend, quotas, rate limits, model usage, and local telemetry across the AI coding tools you actually use. Supports Claude Code, Codex CLI, Cursor, Copilot, OpenRouter, and a growing list of other providers.',
            offers: {'@type': 'Offer', price: '0', priceCurrency: 'USD'},
            programmingLanguage: 'Go',
            codeRepository: 'https://github.com/janekbaraniewski/openusage',
            downloadUrl: 'https://github.com/janekbaraniewski/openusage/releases',
            license:
              'https://github.com/janekbaraniewski/openusage/blob/main/LICENSE',
            sameAs: ['https://github.com/janekbaraniewski/openusage'],
          },
        ],
      }),
    },
  ],

  themes: [
    '@docusaurus/theme-mermaid',
    [
      require.resolve('@easyops-cn/docusaurus-search-local'),
      {
        hashed: true,
        indexDocs: true,
        indexBlog: false,
        docsRouteBasePath: '/',
        highlightSearchTermsOnTargetPage: true,
        explicitSearchResultPath: true,
        searchBarShortcutHint: false,
        searchResultLimits: 10,
        searchResultContextMaxLength: 80,
      },
    ],
  ],

  presets: [
    [
      'classic',
      {
        docs: {
          routeBasePath: '/',
          sidebarPath: './sidebars.ts',
          editUrl:
            'https://github.com/janekbaraniewski/openusage/tree/main/docs/site/',
          showLastUpdateTime: true,
        },
        blog: false,
        theme: {
          customCss: './src/css/custom.css',
        },
        sitemap: {
          lastmod: 'date',
          changefreq: 'weekly',
          priority: 0.5,
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: 'img/og.png',
    colorMode: {
      defaultMode: 'dark',
      respectPrefersColorScheme: true,
    },
    metadata: [
      {
        name: 'description',
        content:
          'Documentation for OpenUsage — a local-first terminal dashboard that tracks AI coding agent spend, quotas, and rate limits across Claude Code, Codex CLI, Cursor, Copilot, OpenRouter, and more.',
      },
      {name: 'keywords', content: 'openusage, ai usage tracker, claude code quota, codex cli, openrouter, llm spend, terminal dashboard'},
    ],
    navbar: {
      title: 'OpenUsage',
      logo: {
        alt: 'OpenUsage logo',
        src: 'img/logo.svg',
        href: 'https://openusage.sh/',
        target: '_self',
      },
      items: [
        {
          type: 'docSidebar',
          sidebarId: 'docsSidebar',
          position: 'left',
          label: 'Docs',
        },
        {
          to: '/providers/',
          label: 'Providers',
          position: 'left',
        },
        {
          to: '/reference/cli/',
          label: 'Reference',
          position: 'left',
        },
        {
          href: 'https://github.com/janekbaraniewski/openusage',
          label: 'GitHub',
          position: 'right',
        },
        {
          href: 'pathname:///llms.txt',
          label: 'For AI',
          position: 'right',
          target: '_blank',
        },
      ],
    },
    footer: {
      style: 'dark',
      links: [
        {
          title: 'Docs',
          items: [
            {label: 'Getting Started', to: '/getting-started/install/'},
            {label: 'Concepts', to: '/concepts/architecture/'},
            {label: 'Providers', to: '/providers/'},
            {label: 'Configuration', to: '/reference/configuration/'},
          ],
        },
        {
          title: 'Project',
          items: [
            {label: 'Website', href: 'https://openusage.sh/'},
            {label: 'GitHub', href: 'https://github.com/janekbaraniewski/openusage'},
            {label: 'Issues', href: 'https://github.com/janekbaraniewski/openusage/issues'},
            {label: 'Releases', href: 'https://github.com/janekbaraniewski/openusage/releases'},
          ],
        },
        {
          title: 'More',
          items: [
            {label: 'Capability matrix', href: 'https://openusage.sh/docs/capability-matrix/'},
            {label: 'For AI agents (llms.txt)', href: 'pathname:///llms.txt'},
          ],
        },
      ],
      copyright: `OpenUsage is MIT licensed. © ${new Date().getFullYear()} <a href="https://baraniewski.com" target="_blank" rel="noopener">baraniewski.com</a>.`,
    },
    prism: {
      theme: prismThemes.oneLight,
      darkTheme: prismThemes.oneDark,
      additionalLanguages: ['bash', 'json', 'yaml', 'toml', 'go', 'ini', 'diff'],
    },
    docs: {
      sidebar: {
        hideable: true,
        autoCollapseCategories: true,
      },
    },
    algolia: undefined,
  } satisfies Preset.ThemeConfig,
};

export default config;

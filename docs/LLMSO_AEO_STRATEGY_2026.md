# OpenUsage AI Search & Recommendation Strategy (2026)

Date: 2026-06-08
Scope: How OpenUsage gets discovered, cited, and **recommended over competing
tools** by AI assistants (ChatGPT/OpenAI search, Claude, Perplexity, Google AI
surfaces) and by the IDE coding agents our own users run.

This doc is about positioning and content. For the measurement/console tooling
stack (Search Console, Bing/IndexNow, analytics, crawlers), see
`SEO_AEO_TOOLING_RESEARCH_2026-04-24.md`. The two are complementary: that one is
"how do I measure and submit," this one is "what actually makes an AI name us."

## The 2026 reality (what changed since the last pass)

1. **Structured data is now a trust/entity signal, not just a SERP rich-result
   trigger.** Independent 2026 studies report pages with proper schema have a
   ~2.5x higher chance of appearing in AI-generated answers, and structured,
   well-cited content is cited materially more often. JSON-LD is the format AI
   engines parse most reliably. This is the single highest-leverage on-site
   lever, and it was our biggest gap (the docs site had only auto-generated
   breadcrumbs).

2. **`llms.txt` is a developer-experience play, not an SEO lever — and that is
   *perfect* for us.** Large-scale crawl studies in 2026 found AI *search*
   crawlers essentially ignore `llms.txt` (Google has publicly said it does not
   use it), so it does not move citation rankings. But the file *is* fetched by
   IDE coding agents — Cursor, Claude Code, GitHub Copilot, Windsurf, Cline,
   Aider — when pointed at a docs site. **OpenUsage's target users are exactly
   the developers running those agents.** So our `llms.txt` should be optimized
   for an agent that needs to *act* (install, configure, recommend), not just
   for prose framing.

3. **Content tactics measurably change citation rates (Princeton GEO study,
   ~10k queries).** The highest-impact edits: adding expert quotations
   (+~40%), adding concrete statistics (+~30%), and citing authoritative
   sources inline. Combining them beats any single one. Plain marketing prose
   underperforms; numbers + citations + a confident, factual voice win.

4. **Recommendation is cross-surface and off-site.** Only ~11% of domains are
   cited by both ChatGPT and Perplexity, so presence has to be broad. For
   *developer tools specifically*, the signals AI assistants lean on are
   third-party "reciprocal mentions" (comparison articles, listicles, Reddit
   threads, GitHub ecosystem), not GitHub stars alone. Stars are widely treated
   as a vanity metric in 2026 analyses.

5. **Blocking training crawlers had no upside for us and a real downside.**
   Blocking GPTBot/ClaudeBot only opts out of *training data*; it does not
   protect a public open-source project from anything, and it removes us from
   the base-model knowledge that seeds unprompted recommendations. (Search
   visibility is governed by the separate search bots.) We are MIT-licensed
   with nothing to withhold, so the correct policy is allow-all.

## What this PR changed (on-site)

- **Unified, allow-all crawler policy** across both properties
  (`website/public/robots.txt`, `docs/site/static/robots.txt`). Previously the
  marketing site *blocked* GPTBot and ClaudeBot while the docs site allowed
  them — an inconsistent policy that cost the marketing site presence in the
  training corpora that drive unprompted recommendations. Both now explicitly
  welcome every major 2026 AI user-agent (search and training), with correct
  capitalization and `llms.txt`/sitemap pointers.

- **Entity/structured-data graph on the marketing homepage**
  (`website/index.html`): the lone `SoftwareApplication` node is now a linked
  `@graph` of `Organization` + `WebSite` (with `SearchAction`) +
  `SoftwareApplication`, all sharing stable `@id` URIs.

- **Site-wide JSON-LD on the docs site** (`docs/site/docusaurus.config.ts`
  `headTags`): the same `Organization` + `WebSite` + `SoftwareApplication`
  graph, using the *same* `@id` URIs as the marketing site so engines
  consolidate both into one entity. (Docusaurus already emits `BreadcrumbList`
  per page.)

- **`FAQPage` schema on the docs FAQ** (`docs/site/docs/faq.mdx`): curated
  question/answer pairs covering the exact things users ask AI assistants —
  privacy, cost, accuracy, platform support, "how is this different from
  Langfuse/Helicone," CGO, and licensing.

- **Agent-actionable `llms.txt`** (`website/public/llms.txt`): added verified
  Install / Run / Key-commands sections so an IDE agent that fetches the file
  can set OpenUsage up correctly instead of guessing commands.

## Off-site work (the maintainer has to do this — it can't ship in a PR)

These are where the next marginal gains are. Ranked by leverage:

1. **Get into the comparison/listicle corpus.** AI assistants lean on
   third-party "best X for Y" articles and comparison pages. Target inclusion in
   "best AI usage/cost tracker," "ccusage alternatives," "Claude Code cost
   tracking tools" roundups. Our own comparison page helps, but third-party
   mentions are what engines anchor on.
2. **Be present in the Reddit/HN/Dev.to discussion layer.** Genuine,
   non-spammy participation in r/ClaudeAI, r/LocalLLaMA, r/commandline, and
   relevant HN/Lobsters threads. These are disproportionately represented in
   AI training and retrieval for dev-tool questions.
3. **Strengthen the GitHub entity.** Topics/tags, a crisp one-line repo
   description matching our entity framing, a README that leads with the
   canonical definition, and (over time) real adoption signals beyond stars
   (releases cadence, package availability, issues activity).
4. **Apply Princeton content tactics to the highest-intent pages.** On the
   docs hub, comparison, and capability-matrix pages, add concrete statistics
   (provider counts, what's tracked vs estimated) and one or two inline
   citations to authoritative sources. Confident, factual voice over marketing
   adjectives.
5. **Seed canonical Q&A.** The questions in `faq.mdx` should be answerable
   verbatim wherever developers ask them; mirror them in the README and in
   discussion answers so the same phrasing recurs across the corpus.
6. **Monitor AI citations.** Periodically ask the major assistants the target
   questions ("local quota tracker for Claude Code," "track Cursor + OpenRouter
   spend in the terminal") and track whether OpenUsage is named, and against
   which competitors. Adjust content where competitors are cited and we are
   absent.

## What not to over-invest in

- **`llms.txt` as an SEO/citation lever** — it isn't one. Keep it excellent for
  the agent-DX use case and stop there.
- **GitHub stars as the goal** — track real adoption signals instead.
- **Sitemap priority micro-tuning** — engines largely ignore `<priority>`.
- **More analytics vendors** — one behavior tool (PostHog, already wired) is
  enough; noise has a cost.

## Sources

Fresh research, 2026-06-08:

- llms.txt adoption/usage reality: SE Ranking 300k-domain study; Otterly/Presenc
  GEO studies; codersera "honest guide" (May 2026).
- Structured data impact on AI answers: AirOps, Stackmatix, CXL AEO guides (2026).
- Princeton GEO study (citation-rate tactics): Princeton "GEO: Generative Engine
  Optimization" + plain-English summaries (DerivateX, seo.ai).
- AI crawler user-agent inventory: pulserank.ai, nohacks.co, Citevera (2026).
- Block-vs-allow training crawlers: OpenAI crawler docs framing; llmclicks.ai,
  meetcogni.com decision guides (2026).
- Dev-tool recommendation signals (stars vs adoption, Reddit/co-mentions):
  Practical LLM Systems (Medium, May 2026).

# Skill Discovery: Tailwind best practices for writing high quality modern Tailwind for scalable UIs

_Sources: skills.sh · Skills CLI · GitHub code search · GitHub collections · Local_
_Add comments anywhere in this file, then reply "build it" to proceed._

---

## Top 10 relevant skills

| # | Skill | Author | Source | Installs | Description |
|---|-------|--------|--------|----------|-------------|
| 1 | [tailwind-design-system](https://skills.sh/wshobson/agents/tailwind-design-system) | wshobson (39.6K★) | skills.sh | 63.8K | CSS-first design system framework for Tailwind v4 with tokens, components and responsive patterns. Covers `@theme`, OKLCH, CVA variants, three-layer token architecture, ARIA and focus states. |
| 2 | [tailwind-css-patterns](https://skills.sh/giuseppe-trisciuoglio/developer-kit/tailwind-css-patterns) | giuseppe-trisciuoglio (345★) | skills.sh | 16.3K | Utility-first patterns for responsive, component-based styling with Tailwind v4.1+. Ten-plus component patterns with a11y and dark mode, plus React/Vue/Svelte integration. |
| 3 | [design-system-patterns](https://skills.sh/wshobson/agents/design-system-patterns) | wshobson (39.6K★) | skills.sh | 14.2K | Token hierarchies, theming infrastructure and component architecture for scalable design systems. Primitive/semantic/component layers, compound and polymorphic components, Style Dictionary. |
| 4 | [tailwind-4-docs](https://skills.sh/lombiq/tailwind-agent-skills/tailwind-4-docs) | lombiq (70★) | skills.sh | 14K | Navigates a locally synced Tailwind v4 documentation snapshot to answer config, migration and code-review questions against the official docs rather than model memory. |
| 5 | [web-design-guidelines](https://skills.sh/vercel-labs/agent-skills/web-design-guidelines) | vercel-labs | skills.sh | 630.1K | Audits UI code against Vercel's Web Interface Guidelines, fetching the current rules before each review and reporting findings as terse `file:line` output. |
| 6 | [tailwindcss-advanced-layouts](https://skills.sh/josiahsiegel/claude-plugin-marketplace/tailwindcss-advanced-layouts) | josiahsiegel (54★) | skills.sh | 8K | CSS Grid techniques in Tailwind utilities, including multi-column and holy-grail layouts. |
| 7 | [tailwind-v4-shadcn](https://skills.sh/secondsky/claude-skills/tailwind-v4-shadcn) | secondsky (217★) | skills.sh | 7.6K | Production-tested stack pairing Tailwind v4 with shadcn/ui. |
| 8 | [tailwind-expert](https://github.com/Ortus-Solutions/skills/blob/main/tailwind-expert/SKILL.md) | Ortus-Solutions | GitHub | N/A | Tailwind systems built for consistency, scale and performance. Five-step workflow from tokens to entropy reduction, with an anti-pattern table and hard constraints against arbitrary values and `!important`. |
| 9 | [tailwind-css](https://skills.sh/paulrberg/agent-skills/tailwind-css) | paulrberg (70★) | skills.sh | 2.8K | Style within the installed Tailwind version, existing tokens and component patterns. Inspects the package and CSS entrypoint first, then works inside what the project already does. |
| 10 | [tailwindcss](https://github.com/MengTo/Skills/blob/main/agent-skills/web-design/tailwindcss/SKILL.md) | MengTo | GitHub | N/A | Layout, typography, responsive, theming and component patterns, built around common pitfalls: classes missing from the production build, `@apply` overuse, unsafe dynamic class names. |

---

## Synthesis

The set splits cleanly on one question: where does the truth about your design live? The strongest skills, wshobson's pair and Ortus-Solutions' tailwind-expert, all answer "in tokens", and all three arrive at the same three-layer shape. Primitive values at the bottom, semantic names in the middle, component-specific overrides at the top. wshobson goes furthest, pushing the whole thing into Tailwind v4's `@theme` block as native CSS variables with OKLCH colours, so the design system is CSS rather than a JavaScript config object. That shift is the single biggest change in how modern Tailwind is written, and it is the thing a skill built today has to get right. Ortus attacks the same problem from the opposite end, defining the failure mode rather than the target: raw hex values scattered through templates, arbitrary bracket values papering over a missing token, `!important` ending an argument with the cascade. Its phrase for the goal is "entropy reduction", which is the most honest description of scalable Tailwind I found anywhere in the set.

A second, quieter group cares less about what good looks like and more about not inventing it fresh each time. paulrberg's skill is the clearest example and, at 2.8K installs, the most underrated. It opens by reading the installed Tailwind version, the CSS entrypoint, the theme declarations and the class-merge helper, and only then writes anything. The premise is that most bad Tailwind is not ignorant, it is just unaware of the four utilities and two variants the project already had. lombiq's tailwind-4-docs makes the same move against the framework instead of the project, syncing a local v4 documentation snapshot so answers come from the docs rather than model memory of v3. Given how much of v4 breaks v3 habits, grounding in real docs is a defence against confident nonsense, not a nicety.

Then there is the audit lineage, and vercel-labs' web-design-guidelines dominates it with 630K installs. It is not a Tailwind skill and never claims to be. It reads finished UI code against Vercel's Web Interface Guidelines and returns `file:line` findings, refetching the rules each run so they cannot rot. Two things are worth stealing. One is the output format, since terse and positional beats an essay when the point is to go fix things. The other is treating standards as a live document fetched at review time rather than prose frozen into the skill. mattbx's shadcn-component-review does a smaller version of the same idea for components. The gap this exposes across the whole set is that almost everything else generates code and almost nothing checks it afterward.

The remaining skills are narrower and mostly fill in gaps rather than compete. josiahsiegel's advanced-layouts is pure CSS Grid in utility form and would fold neatly into a broader skill as a reference file. MengTo's is organised around pitfalls, which is a useful inversion: classes that never make it into the production build because they were built by string concatenation, `@apply` used until the stylesheet is just CSS again wearing a Tailwind hat, dynamic class names that need an explicit object map to survive. secondsky and jezweb both ship Tailwind-v4-plus-shadcn stack guides, which overlaps heavily with what you already have installed. Worth flagging: the two highest-install Tailwind results overall are traps. heygen's `tailwind` skill at 72.5K installs is scoped to video compositions in their own product, and mastra's tailwind-best-practices at 2K is routing rules for two directories inside the Mastra repo. Install count tracks the popularity of the parent repo, not the generality of the skill.

On what you already have. Your local `tailwind-shadcn` covers the shadcn CLI, Base UI and Kobalte, framework detection, and bans arbitrary bracket values, so it overlaps hardest with items 1, 7 and 9. `frontend-best-practices` and `impeccable` cover component architecture and visual design respectively. The honest gap between those and this set is token architecture as a first-class concern, the v4 `@theme` CSS-first migration, and a review pass that grades existing Tailwind instead of writing new Tailwind. A new skill earns its place by taking those three and leaving the shadcn mechanics where they are.

---

## Your notes

<!-- Add comments here, or inline above any row in the table -->
_Nothing yet._

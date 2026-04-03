# Council Verdict: Share Service — From Tool to Team Memory

> **Date:** 2026-04-03 | **Format:** 2-round Council with IterativeDepth + BeCreative | **Members:** Product Strategist, Pragmatist, Critic, User Advocate

---

## The Problem

The share-service (`share.eregistrations.dev`) works technically — it publishes markdown and HTML with a polished viewer, TOC, syntax highlighting, source toggle, dark source view, heading anchors, code copy buttons, and ETag caching. But the team has **not yet perceived its added value**. Adoption is near zero (3 public documents).

---

## Round 1: Why No Adoption?

### Convergence (4/4 agreed)

1. **The tool is an island.** It exists outside every workflow the team uses (Slack, Jira, terminal). Nobody visits `share.eregistrations.dev` unprompted.

2. **3 documents is a graveyard, not a library.** An empty catalog signals abandonment. Volume creates value — there is no network effect below critical mass.

3. **Sharing is push, but there's no pull.** The tool requires deliberate action to publish. No one receives a signal when content appears. Links don't surface where attention lives.

4. **The framing is wrong.** "Document sharing" is infrastructure language. The team doesn't need a publishing platform — they need **findable, presentable, permanent records of work that would otherwise vanish in terminal scrollback.**

### Key Disagreement

| | Critic | Strategist |
|---|---|---|
| Is this the right tool? | Maybe not — if the only use case is "agent output parking lot," it's infrastructure for infrastructure | Yes — but only if repositioned as the **team's AI work ledger** |

### High-Consensus Criteria

| Criterion | Raised by |
|---|---|
| Agent auto-publishes significant outputs without user action | All 4 |
| New document links appear in Slack where the team already looks | All 4 |
| Homepage is an activity feed, not an empty catalog | Strategist, Pragmatist, User Advocate |
| Shared links render rich previews (OpenGraph) in Slack/Jira | User Advocate, Pragmatist |
| Non-CLI team members can discover and browse documents | Critic, Strategist, Pragmatist |
| Documents are tagged by project/country for retrieval | Strategist, User Advocate |

---

## Round 2: The Data Lake Debate

The council explored a deeper idea: **What if share-service became not just a ledger, but a data lake of AI work that can be analyzed?**

### Strategist — "The shelf should read itself"

> Cross-country pattern intelligence is the unlock. When an agent analyzing Guatemala's business registration hits a Determinant edge case, and six months ago an agent solved the identical pattern for Tanzania — today that insight is gone. With an analyzable work lake, you surface it automatically. Share-service stops being a publishing tool and becomes the **feedback loop** for the entire AI-assisted operation.

### Pragmatist — "Ship four fields. The lake fills itself"

> Don't build analytics before you have data. Add `projectTag`, `workType`, `agentSession`, `tokenCost` to every document NOW. The mind-blowing team meeting moment is one GROUP BY query: *"Last month, agents produced 23 migration analyses across 4 countries. Tanzania took 40% of all AI effort."* Analytics earn their build cost only after 100+ rows.

### Critic — "You're building a cathedral on three documents"

> Fatal flaw: a data lake requires governance this team can't staff. Auto-publish without curation creates a **data swamp**, not a data lake. Who tags? Who curates? Who decides what expires? The real answer is a **registry** — strict metadata, expiration dates, project scoping. A registry is governable. A lake at this scale is a fantasy.

### User Advocate — "Call it the team's memory"

> A team lead opens one feed Monday morning and sees what agents produced across 7 countries last week — without attending 7 standups. The value isn't volume, it's **cross-country pattern recognition no single person can hold in their head**. Three countries solved the same fee-calculation problem three different ways. Only the memory surface reveals it.

### The Tension

| | Strategist + Advocate | Critic |
|---|---|---|
| Vision | Institutional memory that compounds | Ungoverned auto-publish = data swamp |
| Scale | "Agents across 12 countries" | "You have 3 documents today" |
| Resolution | The Pragmatist's middle path | Registry with strict metadata |

### Where They Converge

All four agree: **structured metadata is the prerequisite**, not analytics UI.

1. **Pragmatist + Critic**: capture metadata fields now, build analytics later
2. **Strategist + Advocate**: cross-country pattern recognition is the killer value
3. **Critic's constraint accepted by all**: governance (retention, tagging) must be designed in, not bolted on

---

## Recommended Path: Registry First, Memory Later

### Phase 1 — Governed Registry (now)

- Add 4 metadata fields to the publish API: `project`, `type`, `agent_session`, `tags`
- Auto-publish fills the registry (agent hooks in Claude Code workflows)
- Retention policy: documents expire after 90 days unless pinned
- Homepage becomes a filterable activity feed
- OpenGraph meta tags for rich Slack/Jira previews
- Slack notifications for new documents

### Phase 2 — Searchable Memory (at 100+ documents)

- Full-text search across all artifacts
- Filter by project/country, type, date range
- The "Monday morning query": what happened across all projects last week?
- View counts and basic analytics

### Phase 3 — Compounding Intelligence (at 500+ documents)

- Semantic search: "countries that struggled with cost determinants"
- Agent-queryable `/insights` endpoint (agents consult past work before starting)
- Cross-country pattern detection
- Resource allocation visibility (token costs by project)

**The Pragmatist's law applies at every phase:** don't build the next phase until the current one has enough data to justify it.

---

## The One-Liner

> Stop building a sharing tool. Start building the team's memory — where AI work becomes findable, presentable, and permanent. Volume creates value; visibility creates habit; findability creates dependence.

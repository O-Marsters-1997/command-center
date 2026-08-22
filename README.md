# Command Centre

Local app that drives a DAG of tickets across repos to reviewable pull requests
using agents, one worktree per ticket. It launches agents, watches them, pushes
what they produced, reads CI, and shows one page. It never merges.

```
go run ./cmd/cc --config ~/Documents/plain/.claude/command-centre.toml
```

## Docs

| File | What |
|---|---|
| [docs/prd-command-centre.md](docs/prd-command-centre.md) | What we're building and why. Start here. |
| [docs/command-centre-v1.md](docs/command-centre-v1.md) | The design, revision 3. Source of truth for mechanism. |
| [docs/command-centre-v2-buildability-review.md](docs/command-centre-v2-buildability-review.md) | Staff review of revision 2. Four of its six must-fixes are dissolved by revision 3 — see §15. |
| [docs/assessment.md](docs/assessment.md) | Verdict summary, genre comparison, adaptability. Written against revision 2. |

Nothing is built yet. Two things to settle before code — the CI verdict predicate and
Activity-file ownership — plus one upstream change, `tp remove --force`.

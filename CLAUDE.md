<!-- Copy of Noemai-org/docs CLAUDE.md at commit de3ef2c (unmerged docs branch claude/peaceful-mendel-lenchk-d13-open-decisions). The docs original wins on any difference. Re-point to the merged commit once the docs stack lands. -->

@AGENTS.md

<!-- This file imports AGENTS.md so there is one set of agent instructions. It adds no rules. If anything here appears to conflict with AGENTS.md, AGENTS.md and the SoT win. -->

## Claude Code specifics

- Run the session-start routine in AGENTS.md section 3 before the first edit.
- Mirror the task card's acceptance criteria in Claude Code's task list. Tick each one only against observed evidence.
- Do not store project rules or lessons in personal or user-level memory. Lessons go into AGENTS.md section 15 through a PR, so every agent shares them.
- When you use a subagent, give it the task card and the allowlist, and treat its output as unreviewed work.
- If the session's tools cannot reach a path, a repository or a check that the task needs, stop and report. Never work around the gap.

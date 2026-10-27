<!-- Copy of Noemai-org/docs CLAUDE.md at docs commit 53a50a51d199a7f737f655148cc45b7d726134ff. The docs original wins on any difference. Re-point every copy with: python3 tools/vendor/repoint_docs.py --docs <docs checkout> -->

@AGENTS.md

<!-- This file imports AGENTS.md so there is one set of agent instructions. It adds no rules. If anything here appears to conflict with AGENTS.md, AGENTS.md and the SoT win. -->

## Claude Code specifics

- Run the session-start routine in AGENTS.md section 3 before the first edit.
- Mirror the task card's acceptance criteria in Claude Code's task list. Tick each one only against observed evidence.
- Do not store project rules or lessons in personal or user-level memory. Lessons go into AGENTS.md section 15 through a PR, so every agent shares them.
- When you use a subagent, give it the task card and the allowlist, and treat its output as unreviewed work.
- If the session's tools cannot reach a path, a repository or a check that the task needs, stop and report. Never work around the gap.

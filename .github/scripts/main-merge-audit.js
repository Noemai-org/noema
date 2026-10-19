'use strict';
// Decision logic of the main-merge-audit workflow (GOV-22). The workflow and the offline tests in
// .github/tests/ both import this file, so the tested code is the code that runs.
//
// The unit of audit is an ARRIVAL on the default branch, not a commit object:
//   - a push is walked along first parents from `after` back to `before`; every commit on that walk is
//     one arrival. A merge of a pull request is one arrival (the merge commit); the commits it carries
//     are not audited one by one, because the pull request is what holds the review record.
//   - a direct push of N commits is N arrivals, and each one is checked.
// Each arrival is skipped (and the skip is reported, never counted as a pass) when it is not after the
// installation cutoff, or when an issue for it already exists. Otherwise it is mapped to a pull request
// and one of four rules fires: no_pull_request, no_approval, author_approval_only, stale_approval.

const LABEL = 'governance';
// Labels searched for an existing issue (open or closed), so a relabelled issue is never duplicated.
const DEDUPE_LABELS = ['governance', 'governance-historical'];
const MAX_PR_PAGES = 5; // 500 most recently updated closed pull requests are scanned for merge_commit_sha
const ZERO_SHA = /^0+$/;

const RULES = {
  no_pull_request: 'no pull request merged into the default branch has this commit as its merge commit (a direct push)',
  no_approval: 'the pull request has no approving review',
  author_approval_only: "every approving review came from the pull request's own author",
  stale_approval: 'the approving review was given on an earlier head than the commit that was merged (commits were pushed after the approval)',
};

function shortSha(sha) {
  return sha.slice(0, 7);
}

function titleFor(sha) {
  return `Unreviewed merge to main: ${shortSha(sha)}`;
}

// Reviews -> who reviewed, who counts as an approver, and which rule (if any) fires.
// Rule: the latest state of a reviewer decides; COMMENTED and PENDING do not change it. An approval
// counts only from an account of type "User" (a real account) other than the pull request's author.
// A later CHANGES_REQUESTED or DISMISSED from the same reviewer removes the approval. An approval must
// also have been given on the head that was merged: a review object carries the commit it was given on
// (commit_id), and an approval on an earlier head is stale.
function evaluateReviews(reviews, authorLogin, mergedHead) {
  const latest = new Map();
  for (const r of reviews) {
    if (!r.user || r.state === 'COMMENTED' || r.state === 'PENDING') continue;
    latest.set(r.user.login, { login: r.user.login, type: r.user.type || 'User', state: r.state, commit: r.commit_id });
  }
  const reviewers = [...latest.values()];
  const approvals = reviewers.filter((r) => r.state === 'APPROVED');
  const independent = approvals.filter((r) => r.type === 'User' && r.login !== authorLogin);
  const fresh = independent.filter((r) => r.commit === mergedHead);
  let rule = null;
  if (approvals.length === 0) rule = 'no_approval';
  else if (independent.length === 0) rule = approvals.every((r) => r.login === authorLogin) ? 'author_approval_only' : 'no_approval';
  else if (fresh.length === 0) rule = 'stale_approval';
  return { reviewers, approvers: fresh.map((r) => r.login), rule };
}

// The head the pull request actually merged. A merge commit has the merged head as its second parent,
// which no later push can change; a squash or rebase merge has one parent, so the pull request's recorded
// head is used (GitHub does not move the head of a closed pull request).
function mergedHeadOf(commit, pr) {
  if (commit.parents && commit.parents.length > 1) return { sha: commit.parents[1].sha, source: 'second parent of the merge commit' };
  return { sha: pr.head.sha, source: "the pull request's recorded head (single-parent merge)" };
}

async function getAllPages(fn, params) {
  const out = [];
  for (let page = 1; ; page++) {
    const { data } = await fn({ ...params, per_page: 100, page });
    out.push(...data);
    if (data.length < 100) return out;
  }
}

// Commits of before...after (all pages), as a map sha -> commit.
async function commitsBetween(github, { owner, repo, before, after }) {
  const map = new Map();
  let status = null;
  for (let page = 1; ; page++) {
    const { data } = await github.rest.repos.compareCommitsWithBasehead({ owner, repo, basehead: `${before}...${after}`, per_page: 100, page });
    status = data.status;
    for (const c of data.commits) map.set(c.sha, c);
    if (data.commits.length < 100) break;
  }
  return { map, status };
}

// The arrivals of a push, oldest first, and the carried commits that are not arrivals.
async function arrivalsOf(github, { owner, repo, before, after }) {
  if (!before || ZERO_SHA.test(before)) return { arrivals: [after], carried: [], note: 'no previous tip: the head commit is the only arrival' };
  const { map, status } = await commitsBetween(github, { owner, repo, before, after });
  if (status !== 'ahead' || !map.has(after)) {
    return { arrivals: [after], carried: [], note: `the push is not a fast-forward of the previous tip (compare status ${status}): the head commit is the only arrival` };
  }
  const arrivals = [];
  for (let sha = after; sha && sha !== before && map.has(sha); sha = (map.get(sha).parents[0] || {}).sha) arrivals.push(sha);
  const carried = [...map.keys()].filter((sha) => !arrivals.includes(sha));
  return { arrivals: arrivals.reverse(), carried, note: null };
}

// PR lookup: the pull request whose merge_commit_sha is the commit, then the associated-PRs endpoint.
// Either way only a pull request that merged INTO the default branch with this commit as its merge
// commit counts; a pull request that merged into some other branch is not evidence of review.
async function findPullRequest(github, { owner, repo, sha, base }) {
  for (let page = 1; page <= MAX_PR_PAGES; page++) {
    const { data } = await github.rest.pulls.list({ owner, repo, state: 'closed', base, sort: 'updated', direction: 'desc', per_page: 100, page });
    const hit = data.find((p) => p.merged_at && p.merge_commit_sha === sha);
    if (hit) return { pr: hit, method: 'merge_commit_sha (closed pull requests into ' + base + ')' };
    if (data.length < 100) break;
  }
  const associated = await getAllPages((p) => github.rest.repos.listPullRequestsAssociatedWithCommit(p), { owner, repo, commit_sha: sha });
  const hit = associated.find((p) => p.merged_at && p.base.ref === base && p.merge_commit_sha === sha);
  if (hit) return { pr: hit, method: 'associated pull requests endpoint, filtered to base ' + base + ' and merge_commit_sha' };
  return null;
}

async function reportedShas(github, { owner, repo }) {
  const issues = [];
  for (const labels of DEDUPE_LABELS) {
    issues.push(...(await getAllPages((p) => github.rest.issues.listForRepo(p), { owner, repo, labels, state: 'all' })));
  }
  return issues.filter((i) => !i.pull_request);
}

function alreadyReported(issues, sha) {
  return issues.some((i) => i.title === titleFor(sha) || (i.body || '').includes(sha));
}

function renderIssue({ commit, pr, method, author, evaluation, rule, searched, mergedHead }) {
  const reviewers = evaluation && evaluation.reviewers.length
    ? evaluation.reviewers.map((r) => `${r.login} (${r.state}${r.state === 'APPROVED' ? ' on ' + shortSha(r.commit || '0000000') : ''}${r.type !== 'User' ? ', ' + r.type : ''})`).join(', ')
    : 'none';
  return [
    `Commit: ${commit.html_url}`,
    `Pull request: ${pr ? pr.html_url : 'none'}`,
    `Mapped to the pull request by: ${pr ? method : `no pull request found (${searched})`}`,
    `Author: ${author}`,
    `Reviewers found: ${reviewers}`,
    ...(mergedHead ? [`Head that was merged: ${shortSha(mergedHead.sha)} (${mergedHead.source})`] : []),
    '',
    'Rule that fired:',
    `- ${rule}: ${RULES[rule]}`,
    '',
    'This commit is an arrival on the default branch: the merge commit of a pull request, or a commit pushed directly. The commits a merge carries are not audited one by one.',
    'Resolve this issue before the next merge. This audit reports after the fact and cannot stop a merge.',
  ].join('\n');
}

// One arrival -> one decision. action is 'issue', 'pass' or 'skip'; a skip is never a pass.
async function decide(github, ctx, sha, issues) {
  const { owner, repo, base, cutoff } = ctx;
  if (sha === cutoff.sha) return { sha, action: 'skip', reason: 'before_cutoff' };
  const cmp = await github.rest.repos.compareCommitsWithBasehead({ owner, repo, basehead: `${cutoff.sha}...${sha}` });
  if (cmp.data.status !== 'ahead') return { sha, action: 'skip', reason: 'before_cutoff' };
  if (!ctx.ignoreReported && alreadyReported(issues, sha)) return { sha, action: 'skip', reason: 'already_reported' };

  const commit = (await github.rest.repos.getCommit({ owner, repo, ref: sha })).data;
  const found = await findPullRequest(github, { owner, repo, sha, base });
  const searched = `merge_commit_sha scan of up to ${MAX_PR_PAGES * 100} closed pull requests into ${base}, then the associated pull requests endpoint`;
  if (!found) {
    const author = (commit.author && commit.author.login) || commit.commit.author.name;
    const rule = 'no_pull_request';
    return { sha, action: 'issue', rule, pr: null, title: titleFor(sha), body: renderIssue({ commit, pr: null, author, rule, searched }) };
  }
  const { pr, method } = found;
  const reviews = await getAllPages((p) => github.rest.pulls.listReviews(p), { owner, repo, pull_number: pr.number });
  const mergedHead = mergedHeadOf(commit, pr);
  const evaluation = evaluateReviews(reviews, pr.user.login, mergedHead.sha);
  if (!evaluation.rule) return { sha, action: 'pass', pr: pr.number, method, approvers: evaluation.approvers, mergedHead };
  return {
    sha, action: 'issue', rule: evaluation.rule, pr: pr.number, method, title: titleFor(sha),
    body: renderIssue({ commit, pr, method, author: pr.user.login, evaluation, rule: evaluation.rule, searched, mergedHead }),
  };
}

// Evaluate a push. Reads only. Returns { arrivals, carried, note, decisions }.
async function auditPush(github, ctx) {
  const { owner, repo, before, after } = ctx;
  const { arrivals, carried, note } = await arrivalsOf(github, { owner, repo, before, after });
  const issues = await reportedShas(github, { owner, repo });
  const decisions = [];
  for (const sha of arrivals) decisions.push(await decide(github, ctx, sha, issues));
  return { arrivals, carried, note, decisions };
}

async function ensureLabel(github, { owner, repo }) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name: LABEL });
  } catch (e) {
    if (e.status !== 404) throw e;
    await github.rest.issues.createLabel({ owner, repo, name: LABEL, color: 'b60205', description: 'A governance rule was bypassed' });
  }
}

// Entry point used by the workflow. A workflow_dispatch run is always a dry run: it evaluates the
// commit given as input and logs what it would do, and never opens an issue. Its optional input
// evaluate_reported=true evaluates a commit even though an issue for it exists, to show how it maps.
async function run({ github, context, core, cutoff }) {
  const { owner, repo } = context.repo;
  const base = context.payload.repository.default_branch;
  let before;
  let after;
  let dryRun = false;
  let ignoreReported = false;
  if (context.eventName === 'workflow_dispatch') {
    after = context.payload.inputs.sha;
    ignoreReported = context.payload.inputs.evaluate_reported === 'true';
    before = (await github.rest.repos.getCommit({ owner, repo, ref: after })).data.parents[0].sha;
    dryRun = true;
  } else {
    before = context.payload.before;
    after = context.payload.after;
    if (!after || ZERO_SHA.test(after)) return { arrivals: [], carried: [], decisions: [] };
  }
  const result = await auditPush(github, { owner, repo, base, cutoff, before, after, ignoreReported });
  core.info(`${dryRun ? 'DRY RUN. ' : ''}push ${shortSha(before)}...${shortSha(after)}: ${result.arrivals.length} arrival(s), ${result.carried.length} carried commit(s) not audited one by one`);
  if (result.note) core.info(result.note);
  let opened = 0;
  for (const d of result.decisions) {
    const where = d.pr ? `PR #${d.pr} via ${d.method}` : '';
    if (d.action === 'skip') core.info(`SKIPPED ${shortSha(d.sha)}: ${d.reason} (a skip, not a pass)`);
    else if (d.action === 'pass') core.info(`PASS ${shortSha(d.sha)}: ${where}; approved by ${d.approvers.join(', ')} on the merged head ${shortSha(d.mergedHead.sha)}`);
    else if (dryRun) core.info(`WOULD OPEN ISSUE "${d.title}" [${d.rule}]\n${d.body}`);
    else {
      if (opened === 0) await ensureLabel(github, { owner, repo });
      const issue = await github.rest.issues.create({ owner, repo, title: d.title, body: d.body, labels: [LABEL] });
      opened++;
      core.warning(`opened #${issue.data.number}: ${d.title} [${d.rule}]`);
    }
  }
  return result;
}

module.exports = { run, auditPush, evaluateReviews, arrivalsOf, findPullRequest, renderIssue, titleFor, RULES, DEDUPE_LABELS };

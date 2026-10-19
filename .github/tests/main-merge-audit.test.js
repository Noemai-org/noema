'use strict';
// Offline tests of .github/scripts/main-merge-audit.js (GOV-22). No network, no dependencies:
//   node --test .github/tests/
// The fake GitHub below answers from a commit graph, so compare, first-parent walks and the cutoff are
// computed, not scripted. A guard test checks that every github.rest.<ns>.<method> the script calls is a
// real Octokit method (octokit-methods.json), because the first version of the audit called a method that
// does not exist and no test noticed.
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const audit = require('../scripts/main-merge-audit.js');

const REAL = JSON.parse(fs.readFileSync(path.join(__dirname, 'fixtures', 'real-pushes.json'), 'utf8'));
const METHODS = JSON.parse(fs.readFileSync(path.join(__dirname, 'fixtures', 'octokit-methods.json'), 'utf8'));
const OWNER = 'Noemai-org';
const BASE = 'main';

const sha = (name) => Buffer.from(name).toString('hex').padEnd(40, '0').slice(0, 40);

// ---- fake GitHub ---------------------------------------------------------------------------------
function fakeGithub({ commits, prs = [], associated = {}, reviews = {}, issues = [], repo = 'noema' }) {
  const bySha = new Map(commits.map((c) => [c.sha, c]));
  const order = commits.map((c) => c.sha); // newest first, as git rev-list
  const calls = [];
  const created = [];
  let labelExists = true;
  const reach = (start) => {
    const seen = new Set();
    const stack = [start];
    while (stack.length) {
      const s = stack.pop();
      if (seen.has(s) || !bySha.has(s)) continue;
      seen.add(s);
      stack.push(...bySha.get(s).parents);
    }
    return seen;
  };
  const rec = (name, fn) => async (p) => { calls.push({ name, p }); return fn(p); };
  const rest = {
    repos: {
      compareCommitsWithBasehead: rec('compare', ({ basehead, page = 1, per_page = 100 }) => {
        const [b, h] = basehead.split('...');
        const rb = reach(b);
        const rh = reach(h);
        const status = b === h ? 'identical' : rh.has(b) ? 'ahead' : rb.has(h) ? 'behind' : 'diverged';
        const list = order.filter((s) => rh.has(s) && !rb.has(s)).reverse().map((s) => ({ sha: s, parents: bySha.get(s).parents.map((x) => ({ sha: x })) }));
        return { data: { status, commits: list.slice((page - 1) * per_page, page * per_page) } };
      }),
      getCommit: rec('getCommit', ({ ref }) => {
        const c = bySha.get(ref);
        return { data: { sha: ref, html_url: `https://github.com/${OWNER}/${repo}/commit/${ref}`, author: c.login ? { login: c.login } : null, commit: { author: { name: c.author || 'unknown' } }, parents: c.parents.map((x) => ({ sha: x })) } };
      }),
      listPullRequestsAssociatedWithCommit: rec('associated', ({ commit_sha }) => ({ data: associated[commit_sha] || [] })),
    },
    pulls: {
      list: rec('pulls.list', ({ base, page = 1, per_page = 100 }) => {
        const all = prs.filter((p) => p.base.ref === base);
        return { data: all.slice((page - 1) * per_page, page * per_page) };
      }),
      listReviews: rec('listReviews', ({ pull_number, page = 1, per_page = 100 }) => ({ data: (reviews[pull_number] || []).slice((page - 1) * per_page, page * per_page) })),
    },
    issues: {
      listForRepo: rec('listForRepo', ({ labels, page = 1, per_page = 100 }) => ({ data: issues.filter((i) => i.labels.includes(labels)).slice((page - 1) * per_page, page * per_page) })),
      create: rec('issues.create', (a) => { created.push(a); return { data: { number: 100 + created.length } }; }),
      getLabel: rec('getLabel', () => { if (!labelExists) { const e = new Error('Not Found'); e.status = 404; throw e; } return { data: {} }; }),
      createLabel: rec('createLabel', () => { labelExists = true; return { data: {} }; }),
    },
  };
  return { github: { rest }, calls, created, setLabelMissing: () => { labelExists = false; } };
}

// ---- small synthetic graphs ----------------------------------------------------------------------
// root <- CUT <- tip(before) ; feature <- CUT ; merge = merge(tip, feature)
const CUT = { sha: sha('cutoff'), date: '2026-10-01T00:00:00Z' };
function graph({ extra = [] } = {}) {
  const c = (name, parents, extraFields = {}) => ({ sha: sha(name), parents: parents.map(sha), author: 'someone', login: 'someone', ...extraFields });
  return [
    ...extra,
    c('merge', ['tip', 'feature']),
    c('feature', ['cutoff']),
    c('tip', ['cutoff']),
    c('cutoff', ['root']),
    c('root', []),
  ];
}
const pr = (number, author, mergeName, over = {}) => ({
  number, html_url: `https://github.com/${OWNER}/noema/pull/${number}`, user: { login: author, type: 'User' }, merged_at: '2026-10-02T00:00:00Z',
  merge_commit_sha: sha(mergeName), base: { ref: BASE }, ...over,
});
const rev = (login, state, over = {}) => ({ user: { login, type: 'User' }, state, commit_id: sha('feature'), ...over });
const ctx = (before, after) => ({ owner: OWNER, repo: 'noema', base: BASE, cutoff: CUT, before: sha(before), after: sha(after) });
const pushOf = (name) => ({ before: sha('tip'), after: sha(name) });

// ---- 1 to 4: the three rules and a pass ------------------------------------------------------------
test('1. a merge of a reviewed PR, approved by someone other than the author, raises no issue', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('bob', 'APPROVED')] } });
  const r = await audit.auditPush(f.github, ctx('tip', 'merge'));
  assert.deepEqual(r.arrivals, [sha('merge')]);
  assert.deepEqual(r.decisions.map((d) => d.action), ['pass']);
  assert.deepEqual(r.decisions[0].approvers, ['bob']);
  assert.match(r.decisions[0].method, /^merge_commit_sha/);
});

test('2. a merge of a PR approved only by its own author raises author_approval_only', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('alice', 'APPROVED')] } });
  const [d] = (await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions;
  assert.equal(d.action, 'issue');
  assert.equal(d.rule, 'author_approval_only');
  assert.match(d.body, /Reviewers found: alice \(APPROVED\)/);
  assert.match(d.body, /Mapped to the pull request by: merge_commit_sha/);
  assert.match(d.body, /author_approval_only/);
});

test('3. a merge of a PR with no review at all raises no_approval', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: {} });
  const [d] = (await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions;
  assert.equal(d.action, 'issue');
  assert.equal(d.rule, 'no_approval');
  assert.match(d.body, /Reviewers found: none/);
  assert.match(d.body, /Pull request: https:\/\/github.com\/Noemai-org\/noema\/pull\/7/);
});

test('3b. comments alone are not approval', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('bob', 'COMMENTED')] } });
  assert.equal((await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions[0].rule, 'no_approval');
});

test('4. a genuine direct push to main raises no_pull_request, once per pushed commit', async () => {
  const commits = [
    { sha: sha('d2'), parents: [sha('d1')], author: 'mallory', login: 'mallory' },
    { sha: sha('d1'), parents: [sha('tip')], author: 'mallory', login: 'mallory' },
    ...graph().filter((c) => c.sha !== sha('merge')),
  ];
  const f = fakeGithub({ commits });
  const r = await audit.auditPush(f.github, ctx('tip', 'd2'));
  assert.deepEqual(r.arrivals, [sha('d1'), sha('d2')]);
  assert.deepEqual(r.decisions.map((d) => [d.action, d.rule]), [['issue', 'no_pull_request'], ['issue', 'no_pull_request']]);
  assert.match(r.decisions[0].body, /Pull request: none/);
  assert.match(r.decisions[0].body, /Author: mallory/);
});

test('4b. a direct push of a merge commit with no PR is still an issue', async () => {
  const f = fakeGithub({ commits: graph() }); // merge exists, no PR
  const [d] = (await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions;
  assert.deepEqual([d.action, d.rule], ['issue', 'no_pull_request']);
});

// ---- 5 and 6: the unit of audit is the arrival ---------------------------------------------------
function realPush(key, prsOfRepo, reviewsOfRepo, extra = {}) {
  const fx = REAL[key];
  const commits = fx.commits.map((c) => ({ sha: c.sha, parents: c.parents, author: c.author, login: null }));
  const github = fakeGithub({ commits, prs: prsOfRepo, reviews: reviewsOfRepo, repo: fx.repo.split('/')[1], ...extra });
  return { fx, ...github };
}
const DOCS_PR14 = { number: 14, html_url: 'https://github.com/Noemai-org/docs/pull/14', user: { login: 'OladiranOlatunde', type: 'User' }, merged_at: '2026-10-19T10:09:15Z', merge_commit_sha: REAL.docs_push_14.after, base: { ref: 'main' } };
const DOCS_REVIEWS = { 14: [{ user: { login: 'noemai-systems', type: 'User' }, state: 'APPROVED', commit_id: 'c69ea153e7c56d80fb79a4bd24dc828646973f09' }] };
const DOCS_CUT = { sha: REAL.docs_push_14.before, date: '2026-10-17T23:19:48Z' };
const NOEMA_PR16 = { number: 16, html_url: 'https://github.com/Noemai-org/noema/pull/16', user: { login: 'OladiranOlatunde', type: 'User' }, merged_at: '2026-10-19T10:21:38Z', merge_commit_sha: REAL.noema_push_16.after, base: { ref: 'main' } };
const NOEMA_REVIEWS = { 16: [{ user: { login: 'noemai-systems', type: 'User' }, state: 'APPROVED', commit_id: '76a41fcf244deac44ffb1a823c074a61d63e0181' }] };
const NOEMA_CUT = { sha: REAL.noema_push_16.before, date: '2026-10-19T10:15:59Z' };

test('5. a merge carrying many ancestors is exactly one evaluation (the real docs#14 push shape)', async () => {
  const p = realPush('docs_push_14', [DOCS_PR14], DOCS_REVIEWS);
  const r = await audit.auditPush(p.github, { owner: OWNER, repo: 'docs', base: BASE, cutoff: DOCS_CUT, before: p.fx.before, after: p.fx.after });
  assert.equal(p.fx.commits.length - 1, 32, 'the push carried 31 ancestors plus the merge commit');
  assert.deepEqual(r.arrivals, [p.fx.after]);
  assert.equal(r.carried.length, 31);
  assert.equal(r.decisions.length, 1);
  assert.equal(r.decisions[0].action, 'pass');
  assert.equal(p.created.length, 0);
  assert.equal(p.calls.filter((c) => c.name === 'getCommit').length, 1, 'only the merge commit is fetched');
  assert.equal(p.calls.filter((c) => c.name === 'listReviews').length, 1, 'only one pull request is reviewed');
});

test('6. an ancestor whose own PR merged into a non-main branch is not evaluated separately; as an arrival it is not evidence', async () => {
  const stackPr12 = { number: 12, html_url: 'https://github.com/Noemai-org/docs/pull/12', user: { login: 'OladiranOlatunde', type: 'User' }, merged_at: '2026-10-16T19:32:32Z', merge_commit_sha: 'd208c9f5259c5557f8343e6db046b4392bc1d2e8', base: { ref: 'claude/peaceful-mendel-lenchk-pr8-tidy' } };
  const p = realPush('docs_push_14', [DOCS_PR14], DOCS_REVIEWS, { associated: { d208c9f5259c5557f8343e6db046b4392bc1d2e8: [stackPr12] } });
  const r = await audit.auditPush(p.github, { owner: OWNER, repo: 'docs', base: BASE, cutoff: DOCS_CUT, before: p.fx.before, after: p.fx.after });
  assert.ok(r.carried.includes('d208c9f5259c5557f8343e6db046b4392bc1d2e8'));
  assert.ok(!r.decisions.some((d) => d.sha === 'd208c9f5259c5557f8343e6db046b4392bc1d2e8'));
  // If that commit were itself pushed to main, a PR that merged elsewhere is no review of this arrival.
  const found = await audit.findPullRequest(p.github, { owner: OWNER, repo: 'docs', sha: 'd208c9f5259c5557f8343e6db046b4392bc1d2e8', base: BASE });
  assert.equal(found, null);
});

// ---- 7 and 8: cutoff and de-duplication -------------------------------------------------------------
test('7. a commit before the cutoff is skipped, reported as a skip and never as a pass', async () => {
  const older = [{ sha: sha('old2'), parents: [sha('old1')], author: 'x', login: 'x' }, { sha: sha('old1'), parents: [sha('root')], author: 'x', login: 'x' }, ...graph()];
  const f = fakeGithub({ commits: older });
  const r = await audit.auditPush(f.github, { ...ctx('old1', 'old2') });
  assert.deepEqual(r.decisions.map((d) => [d.action, d.reason]), [['skip', 'before_cutoff']]);
  assert.equal(f.created.length, 0);
  const logs = [];
  await audit.run({ github: f.github, context: { repo: { owner: OWNER, repo: 'noema' }, eventName: 'push', payload: { repository: { default_branch: BASE }, before: sha('old1'), after: sha('old2') } }, core: { info: (m) => logs.push(m), warning: (m) => logs.push(m) }, cutoff: CUT });
  assert.ok(logs.some((l) => /SKIPPED .*before_cutoff.*not a pass/.test(l)));
  assert.ok(!logs.some((l) => /^PASS/.test(l)));
});

test('7b. the cutoff commit itself is not audited, and the arrival right after it is', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('bob', 'APPROVED')] } });
  const itself = await audit.auditPush(f.github, { ...ctx('root', 'cutoff') });
  assert.deepEqual(itself.decisions.map((d) => d.reason), ['before_cutoff']);
  const after = await audit.auditPush(f.github, ctx('cutoff', 'tip'));
  assert.notEqual(after.decisions[0].action, 'skip');
});

test('8. a re-run over a commit already reported opens no duplicate (open, closed or relabelled)', async () => {
  const base = { commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: {} };
  for (const issue of [
    { number: 1, title: audit.titleFor(sha('merge')), body: '', labels: ['governance'] },
    { number: 2, title: 'Unreviewed merge to main: renamed', body: `Commit: https://github.com/x/y/commit/${sha('merge')}`, labels: ['governance-historical'] },
  ]) {
    const f = fakeGithub({ ...base, issues: [issue] });
    const r = await audit.auditPush(f.github, ctx('tip', 'merge'));
    assert.deepEqual(r.decisions.map((d) => [d.action, d.reason]), [['skip', 'already_reported']]);
  }
  const fresh = fakeGithub(base);
  assert.equal((await audit.auditPush(fresh.github, ctx('tip', 'merge'))).decisions[0].action, 'issue');
});

// ---- 9: how approvals are read -----------------------------------------------------------------
test('9. an approval later superseded by CHANGES_REQUESTED does not count', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('bob', 'APPROVED'), rev('bob', 'CHANGES_REQUESTED')] } });
  assert.equal((await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions[0].rule, 'no_approval');
});

test('9b. CHANGES_REQUESTED followed by APPROVED counts, a comment after APPROVED does not undo it, a dismissal does', async () => {
  const cases = [
    [[rev('bob', 'CHANGES_REQUESTED'), rev('bob', 'APPROVED')], 'pass'],
    [[rev('bob', 'APPROVED'), rev('bob', 'COMMENTED')], 'pass'],
    [[rev('bob', 'APPROVED'), rev('bob', 'DISMISSED')], 'issue'],
    [[rev('bob', 'APPROVED'), rev('carol', 'CHANGES_REQUESTED')], 'pass'],
  ];
  for (const [reviews, want] of cases) {
    const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: reviews } });
    assert.equal((await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions[0].action, want, JSON.stringify(reviews.map((r) => r.state)));
  }
});

test('9c. only a real account counts: an approval from a Bot does not satisfy the rule', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: { 7: [rev('some-app[bot]', 'APPROVED', { user: { login: 'some-app[bot]', type: 'Bot' } })] } });
  const [d] = (await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions;
  assert.equal(d.rule, 'no_approval');
  assert.match(d.body, /some-app\[bot\] \(APPROVED, Bot\)/);
});

test('9d. a PR that merged into another branch does not count for an arrival on main', async () => {
  const other = pr(7, 'alice', 'merge', { base: { ref: 'some-stack-branch' } });
  const f = fakeGithub({ commits: graph(), prs: [other], associated: { [sha('merge')]: [other] }, reviews: { 7: [rev('bob', 'APPROVED')] } });
  assert.equal((await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions[0].rule, 'no_pull_request');
});

test('9e. the lookup falls back to the associated-PRs endpoint, accepting only a PR into main with this merge commit', async () => {
  const p = pr(7, 'alice', 'merge');
  const f = fakeGithub({ commits: graph(), prs: [], associated: { [sha('merge')]: [p, pr(8, 'alice', 'feature')] }, reviews: { 7: [rev('bob', 'APPROVED')] } });
  const [d] = (await audit.auditPush(f.github, ctx('tip', 'merge'))).decisions;
  assert.equal(d.action, 'pass');
  assert.match(d.method, /associated pull requests endpoint/);
  // A PR that has the commit as its HEAD (not its merge commit) is not a mapping for the merge.
  const f2 = fakeGithub({ commits: graph(), associated: { [sha('merge')]: [pr(8, 'alice', 'feature')] }, reviews: { 8: [rev('bob', 'APPROVED')] } });
  assert.equal((await audit.auditPush(f2.github, ctx('tip', 'merge'))).decisions[0].rule, 'no_pull_request');
});

// ---- run(): issues, dry run, label -------------------------------------------------------------------
function runCtx(eventName, payload) {
  return { repo: { owner: OWNER, repo: 'noema' }, eventName, payload: { repository: { default_branch: BASE }, ...payload } };
}
const quiet = () => { const logs = []; return { logs, core: { info: (m) => logs.push(m), warning: (m) => logs.push(m) } }; };

test('run on a push opens the issue with label governance and creates the label when missing', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: {} });
  f.setLabelMissing();
  const q = quiet();
  await audit.run({ github: f.github, context: runCtx('push', pushOf('merge')), core: q.core, cutoff: CUT });
  assert.equal(f.created.length, 1);
  assert.deepEqual(f.created[0].labels, ['governance']);
  assert.equal(f.created[0].title, `Unreviewed merge to main: ${sha('merge').slice(0, 7)}`);
  assert.ok(f.calls.some((c) => c.name === 'createLabel'));
});

test('run as workflow_dispatch is a dry run: it logs what it would open and opens nothing', async () => {
  const f = fakeGithub({ commits: graph(), prs: [pr(7, 'alice', 'merge')], reviews: {} });
  const q = quiet();
  await audit.run({ github: f.github, context: runCtx('workflow_dispatch', { inputs: { sha: sha('merge') } }), core: q.core, cutoff: CUT });
  assert.equal(f.created.length, 0);
  assert.ok(q.logs.some((l) => /WOULD OPEN ISSUE "Unreviewed merge to main: .*" \[no_approval\]/.test(l)));
});

// ---- 10: the 23 real commits ----------------------------------------------------------------------
// Each of the 23 commits that produced a governance issue on 2026-10-19, with the verdict assigned in the
// diagnosis. role: 'arrival' = an arrival on main (audited); 'carried' = an ancestor carried in by that
// arrival (never audited alone). verdict: why the issue as filed was wrong, or what remains true.
const REAL_23 = [
  // docs, push of PR #14 (merge 7d5b2fa); issues docs#15 to docs#34
  ['docs', '7d5b2fab', 'arrival', 'false positive'],
  ['docs', 'c69ea153', 'carried', 'false positive'],
  ['docs', '29fc3e59', 'carried', 'false positive'],
  ['docs', 'a8e22b3a', 'carried', 'false positive'],
  ['docs', '562ed927', 'carried', 'false positive'],
  ['docs', 'de3ef2cf', 'carried', 'historical'],
  ['docs', 'd208c9f5', 'carried', 'historical'],
  ['docs', '40028a1e', 'carried', 'historical'],
  ['docs', '995e2ff3', 'carried', 'historical'],
  ['docs', 'd69880a1', 'carried', 'historical'],
  ['docs', '55448fef', 'carried', 'historical'],
  ['docs', '29c02cd0', 'carried', 'historical'],
  ['docs', 'b7c9e3eb', 'carried', 'historical'],
  ['docs', '1acabf81', 'carried', 'historical'],
  ['docs', 'fdaf88fe', 'carried', 'historical'],
  ['docs', 'ab25665f', 'carried', 'historical'],
  ['docs', 'd8c64eb0', 'carried', 'historical'],
  ['docs', 'e9b45cf4', 'carried', 'historical'],
  ['docs', '207802ab', 'carried', 'historical'],
  ['docs', 'f1dcad2b', 'carried', 'historical'],
  // noema, push of PR #16 (merge 0fcc15e); issues noema#18 to noema#20
  ['noema', '0fcc15ec', 'arrival', 'false positive'],
  ['noema', '76a41fcf', 'carried', 'false positive'],
  ['noema', '8291cdf0', 'carried', 'false positive'],
];

test('10. the 23 real commits: one arrival per push passes, the other 21 are carried and never audited alone', async () => {
  assert.equal(REAL_23.length, 23);
  const docs = realPush('docs_push_14', [DOCS_PR14], DOCS_REVIEWS);
  const noema = realPush('noema_push_16', [NOEMA_PR16], NOEMA_REVIEWS);
  const results = {
    docs: await audit.auditPush(docs.github, { owner: OWNER, repo: 'docs', base: BASE, cutoff: DOCS_CUT, before: docs.fx.before, after: docs.fx.after }),
    noema: await audit.auditPush(noema.github, { owner: OWNER, repo: 'noema', base: BASE, cutoff: NOEMA_CUT, before: noema.fx.before, after: noema.fx.after }),
  };
  for (const [repo, prefix, role] of REAL_23) {
    const r = results[repo];
    const full = [...r.arrivals, ...r.carried].find((s) => s.startsWith(prefix));
    assert.ok(full, `${repo} ${prefix} is part of the real push`);
    if (role === 'arrival') {
      assert.ok(r.arrivals.includes(full), `${prefix} is the arrival`);
      assert.equal(r.decisions.find((d) => d.sha === full).action, 'pass', `${prefix} is approved, so no issue`);
    } else {
      assert.ok(r.carried.includes(full), `${prefix} is carried by the merge`);
      assert.ok(!r.decisions.some((d) => d.sha === full), `${prefix} is not evaluated on its own`);
    }
  }
  assert.equal(docs.created.length + noema.created.length, 0);
  assert.equal(results.noema.arrivals.length, 1);
  assert.equal(results.noema.carried.length, 2);
});

// ---- the bug class that hid the first defect ------------------------------------------------------------
test('every github.rest method the audit calls is a real Octokit method', () => {
  const source = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'main-merge-audit.js'), 'utf8');
  const used = [...source.matchAll(/github\.rest\.(\w+)\.(\w+)/g)].map((m) => [m[1], m[2]]);
  assert.ok(used.length >= 8);
  for (const [ns, name] of used) assert.ok((METHODS[ns] || []).includes(name), `github.rest.${ns}.${name} does not exist in Octokit`);
  assert.ok(!(METHODS.repos || []).includes('listPullRequestsForCommit'), 'the name the first version used is not a method');
});

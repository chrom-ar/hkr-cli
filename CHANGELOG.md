# Changelog

What changed in each release of `hkr`, for the people who use it.

## Unreleased

- Scans are bounded by money, not the clock: how a scan ended is said in
  words (e.g. "Finished early: HKr stopped sending it back with budget
  left"), a scan paused for making no progress says so, and `scans
  show` says when a scan has no spending ceiling or no time limit.
- `scans start` says when its preset stops early (e.g. "or earlier on a
  confirmed high"), and when there's no spending ceiling, that the
  balance is the limit. `--max-duration` is optional, with no default.
- `hkr schedules create --working-hours 'tue 20:00-23:00'` (repeatable):
  a schedule that launches only in those weekly ranges, each range's end
  the scan's deadline; a range can cross midnight. `schedules list`
  shows them (`Tue 20:00–23:00 · Fri 22:00–Sat 06:00`), and `scans
  show` a running scan's deadline.

## 0.2.2

- `hkr deps list` says which CVEs are known to be exploited (CISA's KEV)
  and how likely exploitation is (FIRST's EPSS); `hkr deps show` adds
  CISA's due date and required action, its SSVC decision points, and
  the CWE ids.

## 0.2.1

- `hkr activity show` on a read of the lock files: `--packages` prints
  every package each project resolved, and a failed read says where it
  stopped (`hkr activity` shows it too, e.g. `failed (clone)`).

## 0.2.0

- `hkr activity show <id>`: one entry as its page in the web app shows
  it. A fix pull request comes with its test runs (exit before → after)
  and its whole patch; a read of the lock files with each project, and
  the ones it couldn't read and why. `hkr open activity <id>` opens the
  page.

## 0.1.3

- `hkr activity` (and `hkr repos activity <repo>`): what HKr did with
  the organization's code, newest first: deliveries from the git host and
  what HKr decided, the pull requests it opened, lock file reads.
  `--repo`, `--kind delivery|pull_request|dependencies`.

## 0.1.2

- `hkr version` prints the version and checks for a newer release right
  away, in CI and for agents too, with the command that upgrades it
  (`--json`: `version`, `commit`, `date`, `latest`, `update_available`,
  `upgrade`).

## 0.1.1

- `hkr login` no longer needs `--setup`: HKr's approval page starts with
  setup changes on, and you narrow the token there. The flag still works,
  and does nothing.

## 0.1.0

- `hkr` is on Homebrew (`brew install chrom-ar/tap/hkr`) and in Chroma's
  apt and dnf repositories; 0.0.1 was only on GitHub's releases page.

## 0.0.1

- `hkr login` signs in by approving a code in the browser; the token is
  kept in the system keychain, or a file only you can read. `hkr logout`
  revokes it, and `hkr whoami` shows who it is and what it can do.
  `HKR_TOKEN` works in CI and for agents.
- `hkr orgs list`, and `hkr use <org>` for a default organization.
- At a terminal, `hkr` says once a day when a newer version is out.
- `hkr findings list` across your domains (`--state`, `--severity`,
  `--domain`, `--class`, `--source`), `show` (`--markdown` for an AI),
  `evidence` (list and download), `fix` (ask for a fix pull request;
  `--dry-run`, `--wait`), `fixes`, `mark-fixed`, `accept --reason` and
  `reopen`. `hkr fixes show` and `open-pr`.
- `hkr domains list`, `show`, `map` (what HKr mapped) and `export` (the
  open findings as one Markdown report). Setting one up: `create`,
  `verify` (publish a DNS record; `--check` exits 1 until it passes),
  `attest` (sign HKr's statement, typed confirmation) and `limits`. A
  domain is named by its root domain, its name or its id.
- `hkr scans list`, `show` (with its timeline), `start` (says where it
  stops and asks; `--dry-run`; limits for one scan; `--follow`),
  `follow` (live steps, status and findings until it ends; exit 4 when
  it failed or `--fail-on high` matches, for CI), `steer`, `wind-down`,
  `stop` and `resume`.
- For organization admins: `hkr repos list|show`, `hkr reviews
  list|show`, and `hkr deps list|show|fix|dismiss|restore` for
  dependency CVEs.
- `hkr open` prints (and opens) a finding's, domain's, scan's or
  repository's web page; `hkr api` makes a raw request to HKr's API.
- `hkr login --setup` asks for a token that may change setup
  (schedules, repository settings and links; you decide on the page),
  and `hkr whoami` says whether a token has it.
- `hkr status` shows HKr's setup checklist: what's done, what's left,
  and the command for the next step (`--check` exits 1 until the
  required steps are done). Setup commands end with that next step.
- `hkr repos connect github|gitlab|bitbucket` opens HKr's page to
  connect a git host (an admin, signed in, in the browser), waits for
  the connection and lists the repositories.
- `hkr repos set` changes reviews on push (branches, paths, how often),
  dependency watching and the fix level (organization admins, with a
  setup token; a fix level asks first, since HKr then spends on its
  own).
- `hkr links list|confirm|reject|add`: which repository deploys or
  defines which host, so fixes know where to open pull requests.
- `hkr guide` prints the setup, step by step, for people and agents.
- `hkr schedules list|create|pause|resume|delete`: scan a domain every
  day, week or month, or once (creating, resuming and deleting ask
  first, and need a setup token).
- When HKr's terms aren't accepted yet, `hkr` says where to accept them,
  in the web app, and exits 3; `scans follow` too. It never accepts them
  for you. A login approved before accepting keeps its token.
- `hkr orgs list` labels its count "OPEN CRITICAL/HIGH", which is what it
  counts. `hkr schedules create` warns when the domain isn't verified
  or attested yet, since HKr skips the scans until it is.

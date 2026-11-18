# Changelog

What changed in each release of `hkr`, for the people who use it.

## Unreleased

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

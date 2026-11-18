# Setting HKr up

HKr tests your domains the way an attacker would, and fixes what it
finds in your code. This is how to get it there, for a person or for
their coding agent. Each step is one command; `hkr status` always says
which one is next.

## The loop

```sh
hkr status                  # what's done, what's left; the last line is "Next: <command>"
hkr status --check          # the same, and exits 1 while a required step is left
hkr status --json           # the state as HKr sends it: step keys and web pages, no commands
```

Run the command on `hkr status`'s `Next:` line (on stderr), then
`hkr status --check` again, until it exits 0. Optional steps (a git host, links, schedules) make fixes and
recurring scans possible; they never block it.

## Signing in

```sh
hkr login --setup           # opens HKr in the browser; the person approves
hkr whoami
```

The person approves the login in their browser and picks the token's
toggles:

- **Read only**: it can look, not change anything.
- **Only <organization>**: other organizations don't exist for it. It
  narrows to the organization selected in the web app, so switch to the
  right one there first.
- **Setup changes**: schedules, repository settings and links; HKr may
  spend on its own. `--setup` pre-sets it; the person decides.

Set up with a token that has setup changes; day to day, use a read-only
one, or one narrowed to the organization, through `HKR_TOKEN`.

## The steps

```sh
hkr domains create shop.example.com              # the domain to test
hkr domains verify shop.example.com              # prints a DNS TXT record to publish
hkr domains verify shop.example.com --check      # exits 1 until HKr sees it
hkr domains attest shop.example.com --name 'Full Name'
hkr scans start shop.example.com --dry-run       # where it would stop, the balance
hkr scans start shop.example.com --follow        # asks first: it spends credit
hkr repos connect github                         # optional: for fix pull requests
hkr links list --status proposed                 # which repository deploys which host
hkr links confirm Ln2Kq2XpAb2m
hkr repos set acme/web --review-on-push on --watch-deps on
hkr schedules create shop.example.com --every week --weekday mon --at 03:00
```

## What only the person can do

An agent stops and asks the person for these:

- **Approving the login** in the browser (`hkr login`).
- **Publishing the DNS record** that `hkr domains verify` prints, if the
  agent can't change DNS itself.
- **The attestation.** `hkr domains attest` records, in the person's
  name, that they have the owner's authorization to test the domain.
  Show them the statement and let them run it. Never pass
  `--statement-accepted` on their behalf without their explicit say-so.
- **Connecting a git host**: `hkr repos connect` opens a page where an
  admin of the organization, signed in, authorizes it.
- **Accepting HKr's terms.** If HKr says the terms aren't accepted,
  stop and ask the person to open the URL it gives (exit 3,
  `terms_not_accepted`). There is no command to accept them, and an
  agent never accepts them for the person.
- **Buying credit**, in the web app (`hkr status` shows the link).
- **Spending**: `hkr scans start`, `hkr findings fix`, a schedule or a
  fix level ask first. Pass `--yes` only when the person said so.

## Then, day to day

```sh
hkr findings list --severity high
hkr findings show Fx7Kq2XpAb2m --markdown
hkr findings fix Fx7Kq2XpAb2m --dry-run
hkr scans follow shop.example.com --fail-on high
```

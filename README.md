# HKr CLI

Use [HKr](https://app.hkr.chrom.ar) from the terminal: what it found in
your domains and code, the evidence and the fix pull requests, the scans
that test your domains, and setting all of it up. One static binary, for
people, their coding agents and CI.

```sh
hkr login
hkr status                                    # what's left to set up
hkr findings list --severity high
hkr findings show Fx7Kq2XpAb2m --markdown > finding.md
hkr scans start shop.example.com --follow
```

## Install

```sh
curl -fsSL https://packages.chrom.ar/hkr/install.sh | sh        # macOS and Linux
brew install chrom-ar/tap/hkr                              # or Homebrew
go install github.com/chrom-ar/hkr-cli/cmd/hkr@latest
```

The installer checks the download against the release's checksums and
puts `hkr` in `/usr/local/bin` when it can, else `~/.local/bin`
(`HKR_INSTALL_DIR` picks another). At a terminal, `hkr` says when a new
version is out, at most once a day (`HKR_NO_UPDATE_NOTIFIER=1` turns it
off). `hkr version` checks now, anywhere (`--json` for scripts and
agents), and says how to upgrade.

On Debian and Ubuntu (after adding Chroma's apt repository once, as in
[chrom-ar/packages](https://github.com/chrom-ar/packages)):
`sudo apt install hkr`; on Fedora and RHEL: `sudo dnf install hkr`.
Every release also has archives and `.deb`, `.rpm` and `.apk` packages
for macOS, Linux and Windows on amd64 and arm64:
[releases](https://github.com/chrom-ar/hkr-cli/releases).

## Signing in

```sh
hkr login            # approve the code in the browser
hkr whoami
hkr logout           # revokes this machine's token and forgets it
```

When you approve, you choose the token's toggles: **read only**, **only
your current organization**, and **setup changes** (schedules,
repository settings and links, which can make HKr spend on its own).
The page starts at the broadest: setup changes on, every organization.
The token is kept in the system keychain, or a file only you can read.
For a local HKr, `--url http://localhost:4000`.

In CI and for agents, set `HKR_TOKEN` (create one in HKr's user
settings, under "Access tokens"), and `HKR_URL` for an HKr other than
app.hkr.chrom.ar.

Which organization: `--org` (or `HKR_ORG`), else the default from
`hkr use acme`, else the only one you're in. `hkr orgs list` lists them.

## Setting up

```sh
hkr status                                        # HKr's checklist, and the next command
hkr guide                                         # the whole setup, step by step
hkr domains create shop.example.com
hkr domains verify shop.example.com               # prints a DNS TXT record to publish
hkr domains verify shop.example.com --check       # exits 1 until HKr sees it
hkr domains attest shop.example.com --name 'Ada Lovelace'
hkr domains limits shop.example.com --preset deep_run --max-spend 60 --max-duration 4h
hkr repos connect github                          # an admin authorizes it in the browser
hkr links list --status proposed                  # which repository deploys which host
hkr links confirm Ln2Kq2XpAb2m
hkr links add acme/web api.shop.example.com
hkr repos set acme/web --review-on-push on --watch-deps on --fix-level critical
hkr schedules create shop.example.com --every week --weekday mon --at 03:00
```

`attest` signs, in your name, that you have the owner's authorization
to test the domain: it shows the statement and asks you to type the
domain. Repositories, links and repository settings are for
organization admins.

## Findings

```sh
hkr findings list                                 # open ones, most severe first
hkr findings list --severity high --domain shop.example.com
hkr findings show Fx7Kq2XpAb2m                    # or paste the finding's URL
hkr findings show Fx7Kq2XpAb2m --markdown         # HKr's finding.md, for an AI
hkr findings evidence Fx7Kq2XpAb2m                # list the files
hkr findings evidence Fx7Kq2XpAb2m F-01/request.txt -o -
hkr findings fix Fx7Kq2XpAb2m --wait              # a fix pull request (costs credit)
hkr findings mark-fixed Fx7Kq2XpAb2m              # confirm HKr's "looks fixed"
hkr findings accept Fx7Kq2XpAb2m --reason 'Only reachable from the VPN'
hkr findings reopen Fx7Kq2XpAb2m
hkr fixes show Fx2Kq2XpAb2m
hkr fixes open-pr Fx2Kq2XpAb2m
```

`--severity high` means high and critical; `high,low` means exactly
those. `--state open|fixed|accepted|all`.

## Scans

```sh
hkr scans start shop.example.com --dry-run        # where it stops, the balance
hkr scans start shop.example.com --instruction 'Orders API first' --follow
hkr scans list shop.example.com
hkr scans show shop.example.com                   # the latest, with its timeline
hkr scans follow shop.example.com --fail-on high  # live, until it ends
hkr scans steer shop.example.com 'Look at the admin panel next.'
hkr scans wind-down shop.example.com
hkr scans stop shop.example.com
hkr scans resume shop.example.com
hkr domains map shop.example.com                  # what HKr mapped
hkr domains export shop.example.com > findings.md
```

A domain is named by its root domain, its name or its id; a scan by its
number (the latest by default). Anything that spends credit says how
much and asks, or needs `--yes`.

`scans follow` prints the timeline, then steps, status changes and
findings live, reconnecting if the connection drops. It exits 4 when the
scan failed or was cancelled, or when `--fail-on` matches a finding it
saw: a CI step can gate on it.

## Code

For organization admins:

```sh
hkr repos list
hkr repos show acme/web
hkr reviews list acme/web
hkr reviews show acme/web Rv2Kq2XpAb2m
hkr deps list                                     # dependency CVEs, worst first
hkr deps show Ex2Kq2XpAb2m
hkr deps fix Ex2Kq2XpAb2m                         # an upgrade pull request (costs credit)
hkr deps dismiss Ex2Kq2XpAb2m
hkr activity                                      # pushes HKr reviewed, PRs it opened, lock file reads
hkr repos activity acme/web                       # the same, for one repository
```

## Using it with an agent

```sh
hkr guide > HKR.md                                # what to tell the agent
hkr findings list --severity high --json
hkr findings show Fx7Kq2XpAb2m --markdown > finding.md
claude "Fix the vulnerability in finding.md. Then run: hkr scans start shop.example.com --dry-run"
```

- `hkr guide` is the setup, written for an agent: the loop (`hkr status
  --json`, run the next step, `hkr status --check` until it exits 0) and
  what only the person can do (approve the login, connect a git host,
  sign the attestation, buy credit, agree to spend).
- Set up with `hkr login`; day to day, give the agent a
  read-only token, or one narrowed to the organization, through
  `HKR_TOKEN`.
- `--json` on any command prints HKr's JSON as is; `scans follow --json`
  prints one event per line. Lists show one page (`--limit` up to 100,
  then `--cursor`), or every page with `--all`. Errors under `--json` are
  `{"error": {"code", "message"}}` on stderr.
- Results go to stdout; hints, progress and warnings to stderr.
- `hkr open finding <id>` prints (and opens) its page in the web app;
  `hkr api <path>` makes a raw request under `/api/v1`.

## Global flags and environment

| Flag | Environment | |
|---|---|---|
| `--url` | `HKR_URL` | Which HKr. Default: the one last signed in to, else app.hkr.chrom.ar |
| `--org` | `HKR_ORG` | Which organization |
| | `HKR_TOKEN` | A personal token; always wins over the stored one |
| `--json` | | HKr's JSON as is |
| `--lang en\|es` | `HKR_LANG` | Language of HKr's messages |
| `-y`, `--yes` | | Don't ask before spending or stopping |
| `-q`, `--quiet` | | Only errors and the essential result |
| `-v`, `--verbose` | `HKR_DEBUG=1` | Requests and timings on stderr; never tokens |
| `--no-color` | `NO_COLOR` | |

`hkr completion bash|zsh|fish|powershell` prints shell completions.

## Exit codes

`0` done; `1` HKr refused or failed (not found, not allowed, validation,
network); `2` a usage mistake; `3` not signed in, a rejected or revoked
token, a token without the access asked for (read-only, or without
setup changes), or HKr's terms not accepted yet (accept them in the web
app; `hkr` never does); `4` (`scans follow`, and `scans start --follow`) the scan
failed or was cancelled, or `--fail-on` matched.

## Development

```sh
make check          # gofmt, tidy, vet, staticcheck, golangci-lint, tests, govulncheck, cross-builds
make build          # bin/hkr
make fixtures-check # the API fixtures match HKr's (../hkr)
make fixtures-sync  # copy them over
make update         # latest dependencies and tools, then make check
make release        # tag the next patch version (BUMP=minor|major, or V=1.2.3); CI publishes
```

The tests run against a fake HKr built from fixtures that HKr renders
from its real controllers.

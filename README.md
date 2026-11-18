# HKr CLI

Use [HKr](https://app.hkr.chrom.ar) from the terminal: what it found in
your domains and code, the evidence and the fix pull requests, and the
scans that test them. One static binary, for people, their coding agents
and CI.

> Work in progress: `SPEC.md` is what it will do. Nothing is released yet.

```sh
hkr login
hkr findings list --severity high
hkr findings show Fx7Kq2XpAb2m --markdown > finding.md
hkr scans start shop.example.com --preset first_look --follow
```

## Exit codes

`0` done; `1` HKr refused or failed (not found, not allowed, validation,
network); `2` a usage mistake; `3` not signed in, a rejected or revoked
token, or a read-only token asked to change something; `4` (`scans
follow` only) the scan failed or was cancelled, or `--fail-on` matched.

## Development

```sh
make check          # gofmt, tidy, vet, staticcheck, golangci-lint, tests, govulncheck, cross-builds
make build          # bin/hkr
make fixtures-check # the API fixtures match HKr's (../hkr)
make fixtures-sync  # copy them over
make update         # latest dependencies and tools, then make check
make release        # tag the next patch version (BUMP=minor|major, or V=1.2.3); CI publishes
```

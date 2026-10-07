# Development

```sh
make check        # every gate CI runs: lint, complexity, dead code, read-only, tidy, zizmor, actionlint, typos, tests, govulncheck
make integration  # tests including the Docker-based ones (mosquitto via testcontainers)
make build        # ./bin/solix-mqtt-bridge for this machine
make help         # all targets
```

Contribution rules for humans and agents live in [AGENTS.md](../AGENTS.md).

## Local stand-ins

Nothing in the test suite or the dev stack talks to a real device:

- `internal/simulator` serves the Solarbank's registers over Modbus TCP and records every function code it receives.
- `internal/openwbfake` validates messages the way openWB does.
- `internal/testbroker` runs mosquitto (optionally with openWB's ACL and a TLS listener) plus a TCP proxy that can cut
  connections to trigger Last Wills.

## Dev stack

```sh
make dev-up     # simulated Solarbank (with injected glitches), mosquitto with openWB's ACL, fake openWB, the bridge
make dev-logs
make dev-down
```

The broker is reachable on `127.0.0.1:18830`, e.g. `mosquitto_sub -p 18830 -t 'others/#' -t 'openWB/#' -v`.

## Log files

`LOG_DIR=tmp/logs` on a local run writes daily files with a line per poll (`msg=poll`: raw and filtered battery
power, state of charge, PV, home load and grid). Keep `LOG_LEVEL=info` so the terminal stays quiet; the files default
to `debug`. `tmp/` is gitignored.

## Watching a real Solarbank

`SOLARBANK_ADDR=<host>:502 make session-up` runs the bridge against the real device with only a local broker on
`127.0.0.1:18831`, and no openWB output. It records every state document to `tmp/session/state.log`. Stop it with
`make session-down`.

## Releases

Pushing a `v*` tag runs the release workflow: goreleaser builds binaries for Linux (amd64, arm64, armv7) and macOS,
archives with an SBOM, and multi-arch images at `ghcr.io/ryckakas/solix-mqtt-bridge`. `make release-check` validates
the goreleaser configuration locally.

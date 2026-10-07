# solix-mqtt-bridge: agent rules

A Go service that reads an Anker SOLIX Solarbank Max AC over local Modbus TCP and publishes its battery data over
MQTT. A generic core (reader, plausibility filters, fresh/stale state) feeds independent output adapters:
- a generic MQTT output for any consumer;
- an opt-in openWB 2.x output, which stops the wallbox and the battery competing for PV surplus (the original
  motivation).

`README.md` has the why. The approved plan, with its decision log and research evidence, lives in `.plans/`
(local only).

## Hard rules (non-negotiable)

- **Plan first for behaviour changes.** Features and changes to how the bridge reads, filters, publishes or logs need
  an approved plan before code (use the planning skill; plans live in `.plans/`, gitignored). Small, well-scoped
  changes (a CI gate, a Makefile target, config, docs) go straight to implementation. If the size is unclear, ask.
- **Isolated testing only.** Develop and test against local stand-ins: the in-process Modbus simulator, a mosquitto
  container, the fake openWB. Never connect to the real openWB or the real Anker device, even though both are
  reachable on the network.
- **Real openWB only with explicit permission, for each request.** Read-only inspection counts as touching it
  (MQTT subscribe, HTTP, ping, version check). Before the first permitted touch, back up its configuration locally
  into `backups/` (gitignored).
- **Real Anker device only with explicit permission.** Writes to its Modbus registers need separate, explicit
  permission each time.
- **Stop before any real-device testing** and ask again, presenting the read-only evidence: the FC04-only reader
  interface, the simulator's function-code recorder test, and the grep gate showing no Modbus write outside
  `internal/simulator`.
- **Never commit or push.** The owner does that.
- **Docs stand on their own.** Don't reference third-party projects that were only used as inspiration. Link only
  sources the code derives from: Anker's official register source and openWB's core repo for the MQTT contract.
- **No default host for any device.** `SOLARBANK_ADDR` and `MQTT_URL` stay required settings, so nothing can
  reach a real device by accident.

## Gates

Run `make check` before considering a change done; green there should mean green in CI. It runs golangci-lint,
bonsai-lint, deadcode, the read-only grep gate (`make readonly`), the go mod tidy drift check (`make tidy-check`),
zizmor (offline), actionlint (with shellcheck), typos, race + integration tests (Docker required), govulncheck and
`go vet`.

- Never report a gate as green that you did not actually run. Fix violations in the same change; flag
  pre-existing ones instead of silently widening a change.
- **Tool availability is itself a gate.** `go version -m "$(command -v golangci-lint)"` must show a Go at least as
  new as go.mod's `go` line. A lint binary built by an older Go analyses nothing and still exits 0.
- `go.mod`'s `toolchain` line is the single place to raise Go. The Makefile and the Dockerfile build arg read it.
- CI pins every tool version exactly; new rules arrive with a deliberate bump. Dependabot moves gomod (including
  the bonsai-lint and deadcode `tool` lines), actions and the Dockerfile base image. These are bumped by hand:
  - the golangci-lint `version:` in `ci.yml`;
  - the zizmor image digest;
  - the govulncheck version (Makefile + `ci.yml`);
  - the actionlint version (Makefile `ACTIONLINT_VERSION`);
  - the ShellCheck version and its SHA-256 (`ci.yml`, actionlint job);
  - the goreleaser version (`ci.yml`, `release.yml`, Makefile `GORELEASER_IMAGE`);
  - the mosquitto image tag (`internal/testbroker`, `deploy/dev/docker-compose.yml`).
- Every `uses:` is pinned to a full commit SHA with its version in a comment. Workflows default to
  `permissions: contents: read`, raised per job, with `persist-credentials: false` on every checkout. CI runs
  zizmor online, which verifies each pin against its action's repository.
- bonsai-lint gates cognitive complexity at 15 with no baseline. Restructure rather than suppress. The only
  suppression is `// bonsai-lint-ignore: <reason>` above the declaration.
- deadcode runs whole-program (`-test -tags=integration`) and fails on any unreachable function. It catches the
  exported-but-uncalled code that golangci-lint's `unused` can't see. Delete dead code rather than keeping it
  "for later". The tool exits 0 on findings, so `make deadcode` fails on any output.
- `//nolint` must name the linter and give a reason (`//nolint:gosec // <why>`). A bare one fails lint.

## Comments

Don't comment. The exception is a short "why" (rationale, a trade-off, the reason for a non-obvious choice or a
device/upstream quirk), and only where it is genuinely needed. Three lines is the ceiling; one is usually right.

Never restate what the code already says. If a comment could be deleted without losing something a reader
couldn't recover from the code, delete it. Where a rule is correct *by omission* (a value deliberately absent from
a switch or a list), pair the "why" with a named test asserting the absence, since a comment alone can't fail.
When a change makes a comment's reasoning untrue, fix or delete the comment in the same change. This applies to Go,
YAML, shell and the Makefile alike; generated code is exempt.

No em dashes (or en dashes used as dashes) in docs, the README or comments. Use a colon, a comma, parentheses or
two sentences instead; write ranges as "0 to 100".

**API docs are the other exception.** Every package and every exported identifier has a doc comment. revive's
`exported` and `package-comments` rules enforce it, because packages are the seams the rest of the module codes
against.
- **The first sentence says what the item is**, starting with its name as Go requires, and stays under about 200
  characters, since tooling lists it beside the name.
- **The rest adds only what the signature can't say:** a unit (W, Wh, %), a range, a sign convention, what a zero
  value means, which register a value comes from, and the "why". Never restate the name or the type.
- **Unexported code keeps the why-only rule.**

## Domain invariants

- **Read-only towards the Solarbank.** Production code reaches Modbus only through an interface that exposes
  FC04 input-register reads. No write function code exists outside `internal/simulator`.
- **The device's sign convention is converted in exactly one place.** Solarbank register 10008 is + discharging,
  − charging. `Snapshot.ChargePowerW` inverts it, and every output publishes charge-positive power (openWB's
  `get/power` uses the same convention).
- **Outputs are independent adapters.** Each has its own MQTT connection and its own Last Will, because one
  connection carries only one will and the outputs need different ones. The core knows nothing about any consumer.
- **Each output owns its stale semantics.**
  - Generic MQTT: availability `offline` (Last Will, stale data, shutdown).
  - openWB never expires battery data, so the openWB output neutralises it itself: `power=0` when stale, as its
    Last Will, and on shutdown.
- **Test stand-ins never link into production.** `internal/simulator`, `internal/openwbfake`, `internal/testbroker`
  and `internal/testcert` are reachable only from tests and the stand-ins' own `cmd/` binaries; depguard enforces
  this.

## Testing conventions

Tests are colocated per package and table-driven. Tests that need Docker (mosquitto via testcontainers) carry the
`integration` build tag. Pure logic (decoders, plausibility filters, the staleness state machine) takes an
injectable clock and needs no network. Modbus behaviour is tested against the in-process simulator on a random
local port, never against a real address.

# solix-openwb-bridge — agent rules

A Go service that reads an Anker SOLIX Solarbank Max AC over local Modbus TCP and publishes its battery data to an
openWB 2.x wallbox over MQTT, so the two stop competing for PV surplus. Read `HANDOFF.md` first for goal, decisions
and open questions.

## Hard rules (non-negotiable)

- **Plan first.** No code before an approved plan (use the planning skill; plans live in `.plans/`, gitignored).
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
bonsai-lint, deadcode, zizmor (offline), typos, race + integration tests (Docker required), govulncheck and
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
  - any container image tag used by tests.
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
- **Sign conventions differ between the two sides; convert in exactly one place.**
  - Solarbank register 10008: battery power, + discharging, − charging.
  - openWB `get/power`: + charging, − discharging.
- **openWB never expires battery data.** The bridge neutralises stale data itself: `power=0` when stale, as the
  MQTT Last Will, and on shutdown.
- **Test stand-ins never link into production.** `internal/simulator` and `internal/openwbfake` are reachable only
  from tests and their own `cmd/` binaries; depguard enforces this.

## Testing conventions

Tests are colocated per package and table-driven. Tests that need Docker (mosquitto via testcontainers) carry the
`integration` build tag. Pure logic (decoders, plausibility filters, the staleness state machine) takes an
injectable clock and needs no network. Modbus behaviour is tested against the in-process simulator on a random
local port, never against a real address.

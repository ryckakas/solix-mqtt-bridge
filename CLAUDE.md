# solix-openwb-bridge — agent rules

Read `HANDOFF.md` first for goal, decisions and open questions.

## Hard rules (non-negotiable)

- **Plan first.** No code before an approved plan (use the planning skill; plans live in `.plans/`).
- **Isolated testing only.** Develop and test against local stand-ins (Docker: mosquitto broker,
  Modbus TCP device simulator, fake openWB subscriber). Never connect to the real openWB or the real
  Anker device, even though both are reachable on the network.
- **Real openWB only with explicit permission**, and only after its current configuration has been
  backed up locally (into `backups/`, which is gitignored). Read-only inspection counts as touching it.
- **Real Anker device only with explicit permission.** Writes to its Modbus registers need separate,
  explicit permission each time.
- **Never commit or push** unless the user asks.
- Docs must stand on their own: don't reference third-party projects that were only used as
  inspiration. Only link sources the code actually derives from (e.g. the register map source).

# Hand-off brief

## Problem

An openWB wallbox and an Anker SOLIX Solarbank Max AC (A17E2, AC-coupled battery) both regulate on
grid power and compete for PV surplus:

- The battery absorbs surplus, grid reads ~0 W, so openWB sees no surplus to charge the EV.
- When the EV charges, the battery sees grid import and discharges into the car.

Neither device knows about the other.

## Goal

A small Go service that reads the Solarbank over its **local Modbus TCP** interface and publishes
battery data to **openWB via MQTT**, so openWB's battery-priority logic can account for it.
Optional later phase: switch the Solarbank to "no discharge" while the EV charges.

```
Solarbank Max AC ──Modbus TCP──► solix-openwb-bridge ──MQTT──► openWB
                 ◄── (phase 2, optional) no-discharge while EV charges ◄──
```

## Decisions so far

- **Language:** Go — single static binary, runs on Raspberry Pi / Linux / Docker.
- **Data source:** local Modbus TCP (no cloud). Enable in Anker App → device settings →
  "Three-Party Control Settings" → Modbus TCP (port 502).
- **Target:** openWB 2.x generic MQTT battery module
  (`openWB/set/mqtt/bat/<id>/get/power`, `.../soc`, energy counters) — version still to be confirmed.
- **Testing:** fully isolated, see `CLAUDE.md`.

## Open questions (ask the user at plan start)

1. openWB software version: 1.9.x or 2.x?
2. v1 scope: read-only, or read + control?
3. Which meter does the Solarbank regulate on, and is the wallbox behind it?
4. Hosting / Go module path (e.g. `github.com/<user>/solix-openwb-bridge`)?

## Research to do

- **Max AC Modbus register map** (unit id, register types, word order, scaling, sign of battery
  power, SoC, energy counters, writable mode / power-limit registers, any keepalive for external
  control). Anker's official Home Assistant integration uses this interface:
  https://github.com/anker-charging/ha-anker-solix-official — check its license before deriving a
  register table from it. Known issue there: SoC jumps on Max AC (issue #138).
- **openWB 2.x MQTT interface:** exact battery topics, sign convention, behaviour on stale data,
  battery-priority settings, topics showing whether an EV is charging, backup/restore procedure.
  The openWB source (github.com/openWB/core) answers most of this.
- **Go libraries:** current, maintained Modbus TCP client + server (for the simulator) and MQTT
  client; check maintenance status and minimum Go version — avoid deprecated APIs.

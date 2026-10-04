# solix-mqtt-bridge

![solix-mqtt-bridge: your Anker Solarbank on MQTT](docs/images/cover.png)

[![CI](https://github.com/ryckakas/solix-mqtt-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/ryckakas/solix-mqtt-bridge/actions/workflows/ci.yml)
![License: MIT](https://img.shields.io/badge/license-MIT-blue)

Reads an Anker SOLIX Solarbank Max AC over its local Modbus TCP interface and publishes its battery data over MQTT:

- **Generic MQTT output** for any consumer (Home Assistant, Node-RED, ioBroker, …).
- **openWB 2.x output** (opt-in), feeding openWB's generic MQTT battery module.

```
Solarbank Max AC ──Modbus TCP, FC04 only──► solix-mqtt-bridge ──MQTT──► generic topics (any consumer)
                                                              └─MQTT──► openWB (opt-in)
```

What it guarantees:

- **Local only.** It talks to the device on your LAN and needs no cloud account or API key.
- **Read-only.** It only ever sends Modbus *read input registers* (function code 04). No write request exists
  outside the test simulator, and a CI gate enforces that.
- **Filtered.** Known firmware glitches are caught before they reach your consumers (see
  [Plausibility filters](#plausibility-filters)).
- **Honest about outages.** When the device stops answering, the generic output reports itself unavailable and the
  openWB output reports 0 W. Last Wills cover a crash or a network failure of the bridge itself.

## Why

An openWB wallbox and an AC-coupled Solarbank both regulate on grid power, so they compete for PV surplus:

- The battery absorbs the surplus, grid reads ~0 W, and openWB sees nothing to charge the EV with.
- When the EV draws more than PV delivers, the battery sees grid import and discharges into the car.

Once openWB knows the battery's power and state of charge, its battery-priority logic can account for it. With
Ladepriorität `ev_mode`:

- the battery's charging power counts as surplus the EV may take; as the EV ramps up, the battery sees less export
  and backs off;
- battery discharge is subtracted, so the EV isn't charged from the battery in PV mode.

This assumes the Solarbank's CT clamps sit at the grid connection, with PV, wallbox and house behind them.

## Quick start

1. **Enable Modbus TCP on the Solarbank.** Anker app → device → gear icon → *Three-Party Control Settings* →
   *Modbus TCP*. Give the device a fixed IP address in your router.
2. **Check that the bridge can read it.** Probe mode reads once, prints JSON and exits; it needs no MQTT:

   ```sh
   SOLARBANK_ADDR=192.0.2.10 solix-mqtt-bridge -probe
   ```

3. **Run it** with at least one output configured.
   - **Docker:** see [`deploy/examples/docker-compose.yml`](deploy/examples/docker-compose.yml) and
     [`solix-mqtt-bridge.env`](deploy/examples/solix-mqtt-bridge.env). Images for `linux/amd64`, `linux/arm64`
     and `linux/arm/v7` are at `ghcr.io/ryckakas/solix-mqtt-bridge`.
   - **Binary + systemd:** release archives contain the binary and
     [`solix-mqtt-bridge.service`](deploy/examples/solix-mqtt-bridge.service).

## Configuration

All settings are environment variables. No device or broker address has a default.

| Variable | Default | Purpose |
|---|---|---|
| `SOLARBANK_ADDR` | **required** | Solarbank Modbus endpoint, `host` or `host:port` (port 502 if omitted) |
| `SOLARBANK_UNIT_ID` | `1` | Modbus unit id |
| `SOLARBANK_ALLOW_ANY_MODEL` | `false` | skip the check that the device reports model `A17E2` |
| `POLL_INTERVAL` | `5s` | how often the device is read |
| `MODBUS_TIMEOUT` | `3s` | connect and per-request timeout |
| `STALE_AFTER` | `30s` | how long after the last good read the data counts as stale |
| `SOC_JUMP_CONFIRM` | `10m` | how long an implausible state-of-charge jump must persist before it is accepted |
| `MQTT_URL` | none | enables the **generic output**: `tcp://`, `mqtt://`, `ssl://` or `mqtts://` |
| `MQTT_BASE_TOPIC` | `solix-mqtt-bridge` | topic prefix of the generic output |
| `MQTT_CLIENT_ID` | `solix-mqtt-bridge` | client id of the generic output |
| `MQTT_USERNAME`, `MQTT_PASSWORD` | none | broker credentials |
| `MQTT_CA_FILE` | none | PEM CA bundle to verify a TLS broker |
| `MQTT_TLS_INSECURE` | `false` | skip TLS certificate verification |
| `OPENWB_MQTT_URL` | none | enables the **openWB output**: openWB's broker |
| `OPENWB_BAT_ID` | required with `OPENWB_MQTT_URL` | component id of openWB's MQTT battery |
| `OPENWB_PUBLISH_COUNTERS` | `false` | also publish lifetime energy totals (see [openWB setup](#openwb-setup)) |
| `OPENWB_MQTT_CLIENT_ID` | `solix-mqtt-bridge-openwb` | client id of the openWB output |
| `OPENWB_MQTT_USERNAME`, `OPENWB_MQTT_PASSWORD`, `OPENWB_MQTT_CA_FILE`, `OPENWB_MQTT_TLS_INSECURE` | none | as above, for openWB's broker |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |

At least one of `MQTT_URL` and `OPENWB_MQTT_URL` is required. Each output has its own connection, so they can point
at different brokers. The process exits with code 2 on invalid configuration and lists every problem.

## Generic MQTT output

Everything is published retained, at QoS 1, under `MQTT_BASE_TOPIC`:

| Topic | Payload |
|---|---|
| `<base>/availability` | `online` while the data is fresh, otherwise `offline`; the Last Will also sets it to `offline` |
| `<base>/state` | JSON document with everything below, see the example |
| `<base>/charge_power_w` | battery power in W: **positive while charging**, negative while discharging (filtered) |
| `<base>/soc_percent` | state of charge, 0 to 100 (filtered) |
| `<base>/battery_status` | `standby`, `charging`, `discharging` or `sleep` |
| `<base>/pv_power_w` | the Solarbank's own PV input in W |
| `<base>/third_party_pv_power_w` | third-party PV in W |
| `<base>/home_load_w` | home load in W |
| `<base>/grid_power_w` | grid power at the Solarbank's CT in W, positive while importing |
| `<base>/charged_total_wh`, `<base>/discharged_total_wh` | lifetime energy in Wh (100 Wh steps), once valid |

The per-field topics are only updated while the data is fresh. When `availability` is `offline`, treat them as
the last known values.

```json
{
  "status": "fresh",
  "last_good_read": "2026-10-04T12:00:00Z",
  "device": { "model": "A17E2", "serial": "APZ1DMWH0000000001", "firmware": "v1.0.1.14" },
  "battery": {
    "charge_power_w": 1500, "soc_percent": 63, "status": "charging",
    "charged_total_wh": 1234500, "discharged_total_wh": 1100000
  },
  "raw": {
    "battery_power_w": -1500, "pv_power_w": 2000, "third_party_pv_power_w": 0, "home_load_w": 500,
    "grid_power_w": -10, "soc_percent": 63, "status": 1, "max_charge_power_w": 2500,
    "max_discharge_power_w": 2500, "capacity_wh": 5000, "section_status": 1, "section_soc_percent": 63,
    "charged_total_wh": 1234500, "discharged_total_wh": 1100000
  },
  "filter_rejections": { "soc-jump": 3 }
}
```

- `status` is `waiting` before the first read, then `fresh` or `stale`.
- `battery` holds the filtered values.
- `raw` holds the unfiltered register values in the device's own convention. Note that `raw.battery_power_w` is
  positive while *discharging*.

## openWB setup

The openWB output writes to openWB's generic MQTT battery module. It works with openWB 2.x; this was checked
against the 2.2.3 source. In openWB:

1. *Konfiguration → Geräte und Komponenten*: add a device of vendor *Generisch*, type *MQTT*, then a component
   *Speicher*. Its component id goes into `OPENWB_BAT_ID`.
2. *Lastmanagement → Struktur*: place the battery under the grid (EVU) counter.
3. Set *Maximale Leistung des Speichers*. openWB then rejects implausible power values itself.
4. Leave *Speicherleistung steuerbar* at *Nein*. The bridge never controls the battery.
5. *Ladeeinstellungen → PV-Laden*: choose *Ladepriorität* `ev_mode` (EV first) to stop the wallbox and the battery
   competing.

Point `OPENWB_MQTT_URL` at openWB's broker. Port 1883 accepts anonymous clients by default. If openWB's user
management is on, that port is closed: use `ssl://<openwb>:8883` with a user that has the battery's MQTT input
role, plus either `OPENWB_MQTT_TLS_INSECURE=true` or the CA of openWB's self-signed certificate.

Notes:

- **Values are published non-retained.** openWB validates them and stores them itself.
- **openWB never expires battery data.** The output publishes 0 W when the data goes stale, as its Last Will, and
  on shutdown, so openWB never keeps counting a battery it can no longer see.
- **Energy counters are off by default.** The Solarbank counts lifetime energy in 100 Wh steps. While *Maximale
  Leistung des Speichers* is set, openWB's peak filter rejects steps that large, so let openWB integrate the energy
  from power instead. Enable `OPENWB_PUBLISH_COUNTERS` only if that setting is 0.
- **The generic output can share openWB's broker.** openWB lets anonymous clients write under `others/`, so set
  `MQTT_BASE_TOPIC=others/solix-mqtt-bridge`.

## How it behaves

- **Polling.** One persistent Modbus connection, one request at a time, two block reads per poll. After any
  failure the bridge reconnects on the next poll and re-reads the device identity.
- **Freshness.** Data is fresh until `STALE_AFTER` passes without a good read. The Solarbank's Modbus server is
  known to stop when the device loses its internet connection, and to return with it; the bridge rides that out.
- **Model check.** The bridge refuses to run against a device that doesn't report `A17E2`. Anker's integration
  uses the same register map for the Solarbank Max, Solarbank 4 E5000 Pro, XE and XE AC; untested here, but
  `SOLARBANK_ALLOW_ANY_MODEL=true` lets you try.

### Plausibility filters

Each filter guards against a firmware glitch reported for this device:

| Filter | Glitch | Handling |
|---|---|---|
| `zero-power` | battery power reads exactly 0 W for one poll while the status says charging or discharging | keep the last power for up to 2 polls |
| `soc-jump` | state of charge jumps further than the battery could (e.g. 16 % → 85 % for minutes) | keep the last value; accept the new level once it has persisted for `SOC_JUMP_CONFIRM` |
| `soc-range` | state of charge above 100 % | keep the last value |
| `charged-total`, `discharged-total` | lifetime totals read 0 or run backwards (e.g. during a firmware update) | keep the last accepted value |

The physical limit for `soc-jump` comes from the device's own live power limits and rated capacity. Every
rejection is logged and counted in `<base>/state`.

## Development

```sh
make check      # every gate CI runs: lint, complexity, dead code, read-only, zizmor, typos, tests, govulncheck
make dev-up     # local stand-ins: simulated Solarbank, mosquitto with openWB's ACL, fake openWB, the bridge
make dev-logs
make dev-down
```

Everything is tested against local stand-ins; nothing in the test suite or the dev stack talks to a real device.
- `internal/simulator` serves the Solarbank's registers over Modbus TCP and records every function code it
  receives.
- `internal/openwbfake` validates messages the way openWB does.
- Integration tests (`make integration`, needs Docker) run mosquitto via testcontainers.

The dev stack's broker is reachable on `127.0.0.1:18830`, e.g.
`mosquitto_sub -p 18830 -t 'others/#' -t 'openWB/#' -v`.

## License

MIT, see [LICENSE](LICENSE). The Solarbank register map is derived from Anker's official Home Assistant integration
([anker-charging/ha-anker-solix-official](https://github.com/anker-charging/ha-anker-solix-official)), MIT; see
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES). The openWB topic contract follows
[openWB/core](https://github.com/openWB/core).

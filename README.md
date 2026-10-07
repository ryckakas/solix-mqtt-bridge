# solix-mqtt-bridge

![solix-mqtt-bridge: your Anker Solarbank on MQTT](docs/images/cover.png)

[![CI](https://github.com/ryckakas/solix-mqtt-bridge/actions/workflows/ci.yml/badge.svg)](https://github.com/ryckakas/solix-mqtt-bridge/actions/workflows/ci.yml)
![License: MIT](https://img.shields.io/badge/license-MIT-blue)

Reads an Anker SOLIX Solarbank Max AC over its local Modbus TCP interface and publishes its battery data over MQTT:
as generic topics for any consumer (Home Assistant, Node-RED, ioBroker, …) and, optionally, to an openWB wallbox.

```
Solarbank Max AC ──Modbus TCP, FC04 only──► solix-mqtt-bridge ──MQTT──► generic topics (any consumer)
                                                              └─MQTT──► openWB 2.2+ (opt-in)
```

- **Local and read-only.** It needs no cloud account and only ever sends Modbus *read* requests, enforced in CI.
- **Filtered.** Known firmware glitches (0 W blips, state-of-charge jumps, counters running backwards) are caught
  before they reach your consumers.
- **Honest about outages.** When the device stops answering, the generic output goes `offline` and openWB gets 0 W.
  Last Wills cover a crash of the bridge itself.

<details>
<summary><b>Why this exists</b></summary>

An openWB wallbox and an AC-coupled Solarbank both regulate on grid power, so they compete for PV surplus:

- The battery absorbs the surplus, grid reads ~0 W, and openWB sees nothing to charge the EV with.
- When the EV draws more than PV delivers, the battery sees grid import and discharges into the car.

Once openWB knows the battery's power and state of charge, its battery-priority logic can account for it. With the
vehicle-first *Ladepriorität* (`ev_mode`):
- the battery's charging power counts as surplus the EV may take; as the EV ramps up, the battery backs off;
- battery discharge is not fed into the EV in PV mode.

This assumes the Solarbank's CT clamps sit at the grid connection, with PV, wallbox and house behind them.

</details>

## Quick start

1. **Enable Modbus TCP on the Solarbank:** Anker app → device → gear icon → *Three-Party Control Settings* (or
   *Communication Settings*) → *Modbus TCP*. Give the device a fixed IP address in your router.
2. **Check that the bridge can read it.** Probe mode reads once, prints JSON and exits; it needs no MQTT:

   ```sh
   SOLARBANK_ADDR=<solarbank-ip> solix-mqtt-bridge -probe
   ```

3. **Run it** with at least one output configured:
   - **Docker:** [`deploy/examples/docker-compose.yml`](deploy/examples/docker-compose.yml) with
     [`solix-mqtt-bridge.env`](deploy/examples/solix-mqtt-bridge.env); images for amd64, arm64 and arm/v7 are at
     `ghcr.io/ryckakas/solix-mqtt-bridge`.
   - **Binary + systemd:** release archives contain the binary and
     [`solix-mqtt-bridge.service`](deploy/examples/solix-mqtt-bridge.service).
   - **On a Raspberry Pi, step by step:** see [Running it on a Raspberry Pi](docs/raspberry-pi.md).

## Configuration

All settings are environment variables; no device or broker address has a default. A typical setup:

```sh
SOLARBANK_ADDR=<solarbank-ip>                # the Solarbank (port 502 if omitted)
MQTT_URL=tcp://<broker-ip>:1883              # generic output, any broker
OPENWB_MQTT_URL=tcp://<openwb-ip>:1883       # openWB output (optional)
OPENWB_BAT_ID=<component id>                 # openWB's MQTT battery component id
```

<details>
<summary>All settings</summary>

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
| `OPENWB_PUBLISH_COUNTERS` | `false` | also publish lifetime energy totals (see [openWB](docs/openwb.md)) |
| `OPENWB_MQTT_CLIENT_ID` | `solix-mqtt-bridge-openwb` | client id of the openWB output |
| `OPENWB_MQTT_USERNAME`, `OPENWB_MQTT_PASSWORD`, `OPENWB_MQTT_CA_FILE`, `OPENWB_MQTT_TLS_INSECURE` | none | as above, for openWB's broker |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `LOG_FORMAT` | `text` | `text` or `json`, for the console and the log files |
| `LOG_DIR` | none | enables **file logging**: one file per day in this directory, created if missing |
| `LOG_RETENTION_DAYS` | `7` | full days of log files kept before today's (1 to 365); older files are deleted |
| `LOG_FILE_LEVEL` | `debug` | minimum level in the log files; `debug` adds one line per poll with raw and filtered values |

At least one of `MQTT_URL` and `OPENWB_MQTT_URL` is required. Each output has its own connection, so they can point
at different brokers. The process exits with code 2 on invalid configuration and lists every problem.

</details>

## Documentation

| Page | Contents |
|---|---|
| [Generic MQTT output](docs/mqtt-output.md) | topics, the JSON state document, availability |
| [Raspberry Pi](docs/raspberry-pi.md) | step-by-step install on a dedicated Pi, as a systemd service or with Docker |
| [openWB](docs/openwb.md) | setup in openWB, broker access, running the bridge on openWB itself |
| [How it works](docs/how-it-works.md) | polling, freshness, plausibility filters, the read-only guarantee |
| [Development](docs/development.md) | make targets, local stand-ins, dev stack, releases |

## License

MIT, see [LICENSE](LICENSE). The Solarbank register map is derived from Anker's official Home Assistant integration
([anker-charging/ha-anker-solix-official](https://github.com/anker-charging/ha-anker-solix-official)), MIT; see
[THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES). The openWB topic contract follows
[openWB/core](https://github.com/openWB/core).

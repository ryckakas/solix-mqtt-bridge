# How it works

```
Solarbank ─Modbus FC04─► reader ─► plausibility filters ─► fresh/stale state ─┬─► generic MQTT output
                                                                               └─► openWB output
```

Each output has its own MQTT connection and its own Last Will, because one connection carries only one will and the
outputs need different ones: `offline` for the generic output, 0 W for openWB.

## Reading the device

- **Read-only.** The bridge only ever sends Modbus *read input registers* (function code 04). The reader's code can't
  express a write, a CI gate fails on any Modbus write call outside the test simulator, and every test asserts that
  the simulated device received nothing but reads.
- **Polling.** One persistent connection, one request at a time, two block reads per poll (`POLL_INTERVAL`, default
  5 s). After any failure the bridge reconnects on the next poll and re-reads the device identity.
- **Model check.** The bridge refuses to run against a device that doesn't report model `A17E2`. Anker's integration
  uses the same register map for the Solarbank Max, Solarbank 4 E5000 Pro, XE and XE AC; untested here, but
  `SOLARBANK_ALLOW_ANY_MODEL=true` lets you try.

## Freshness

Data is fresh until `STALE_AFTER` (default 30 s) passes without a good read. While stale, the generic output reports
itself `offline` and the openWB output publishes 0 W. The Solarbank's Modbus server is known to stop when the device
loses its internet connection and to return with it; the bridge rides that out and recovers on its own.

## Plausibility filters

Each filter guards against a firmware glitch reported for this device:

| Filter | Glitch | Handling |
|---|---|---|
| `zero-power` | battery power reads exactly 0 W for one poll while the status says charging or discharging | keep the last power for up to 2 polls if it points the way the status says; at a start or a direction change the status moves first, so 0 W passes through |
| `soc-jump` | state of charge jumps further than the battery could (e.g. 16 % → 85 % for minutes) | keep the last value; accept the new level once it has persisted for `SOC_JUMP_CONFIRM` (default 10 min) |
| `soc-range` | state of charge above 100 % | keep the last value |
| `charged-total`, `discharged-total` | lifetime totals read 0 or run backwards (e.g. during a firmware update) | keep the last accepted value |

The physical limit for `soc-jump` comes from the device's own live power limits and rated capacity. Every rejection
is logged (a warning when a filter starts firing, debug while it keeps firing) and counted in the generic output's
state document.

## Register map

The register map is derived from Anker's official Home Assistant integration; see
[THIRD_PARTY_NOTICES](../THIRD_PARTY_NOTICES). It is big-endian with the high word first, on unit id 1, port 502.

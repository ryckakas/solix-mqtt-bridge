# Generic MQTT output

Enabled by `MQTT_URL`. Everything is published **retained, at QoS 1**, under `MQTT_BASE_TOPIC` (default
`solix-mqtt-bridge`).

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

## Availability and stale data

`availability` is `online` only while the data is fresh, meaning the last good read is younger than `STALE_AFTER`. It
turns `offline` when the device stops answering, when the bridge shuts down, and through the Last Will if the bridge
crashes or loses its network.

The per-field topics are only updated while the data is fresh. When `availability` is `offline`, treat them as the
last known values.

## State document

`<base>/state` carries everything in one JSON document:

- `status` is `waiting` before the first read, then `fresh` or `stale`.
- `battery` holds the filtered values, with power positive while charging.
- `raw` holds the unfiltered register values in the device's own convention. Note that `raw.battery_power_w` is
  positive while *discharging*.
- `filter_rejections` counts how often each [plausibility filter](how-it-works.md#plausibility-filters) fired since
  start.

<details>
<summary>Example</summary>

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

</details>

## On an openWB broker

openWB lets anonymous clients write only under `others/`, so set `MQTT_BASE_TOPIC=others/solix-mqtt-bridge` when the
generic output shares openWB's broker.

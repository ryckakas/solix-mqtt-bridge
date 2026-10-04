# openWB

The openWB output (enabled by `OPENWB_MQTT_URL` and `OPENWB_BAT_ID`) writes to openWB's generic MQTT battery module.
It needs **openWB 2.2 or newer** and was checked against the 2.2.3 source. openWB 2.1 and older take MQTT battery data
on different topics, so update openWB first.

## Setup in openWB

1. *Konfiguration → Geräte und Komponenten*: add a device of vendor *Generisch*, type *MQTT*, then a component
   *Speicher*. Its component id goes into `OPENWB_BAT_ID`. The component's help text shows it inside the example
   topics: `openWB/set/mqtt/bat/5/get/power` means id 5.
2. *Lastmanagement → Struktur*: place the battery directly under the grid (EVU) counter, next to the charge point
   and the inverter.
3. Set *Maximale Leistung des Speichers* (in kW; the Solarbank Max AC reports 3 kW). openWB then rejects implausible
   power values itself.
4. Leave *Speicherleistung steuerbar* at *Nein*. The bridge never controls the battery.
5. *Ladeeinstellungen → PV-Laden*: choose the *Ladepriorität* that puts vehicles first (`ev_mode`) to stop the
   wallbox and the battery competing.

Until the bridge publishes, openWB shows a missing-data fault on the new battery. That is harmless: openWB leaves a
faulted battery out of its calculations.

## Broker access

Point `OPENWB_MQTT_URL` at openWB's broker, `tcp://<openwb>:1883`. That port accepts anonymous clients by default.

If openWB's user management is on, port 1883 is closed. Use `ssl://<openwb>:8883` with a user that has the battery's
MQTT input role (`OPENWB_MQTT_USERNAME`, `OPENWB_MQTT_PASSWORD`), plus either `OPENWB_MQTT_TLS_INSECURE=true` or the CA
of openWB's self-signed certificate in `OPENWB_MQTT_CA_FILE`.

## What the output publishes

To `openWB/set/mqtt/bat/<id>/get/power` and `…/soc`, non-retained (openWB validates the values and stores them
itself):

- **Power** in W, positive while charging.
- **0 W whenever the data is stale**, as the Last Will, and on shutdown. openWB never expires battery data, so
  without this it would keep counting a battery it can no longer see.

<details>
<summary>Why energy counters are off by default</summary>

The Solarbank counts lifetime energy in 100 Wh steps. While *Maximale Leistung des Speichers* is set, openWB's peak
filter rejects counter steps that large within one control interval. Without counters, openWB integrates the energy
from power itself. Enable `OPENWB_PUBLISH_COUNTERS` only if *Maximale Leistung des Speichers* is 0.

</details>

## Running it on openWB

If you have shell access to your openWB (for example because you installed it on your own Raspberry Pi), the bridge
can run on openWB itself and reach the broker on `localhost`. Follow
[option A of the Raspberry Pi guide](raspberry-pi.md#option-a-binary-and-systemd) on openWB, with these differences:

- **Binary**: `uname -m` prints `aarch64` (use `linux_arm64`) or `armv7l` (use `linux_armv7`). The `linux_armv7`
  build runs in both cases.
- **Configuration**: `OPENWB_MQTT_URL=tcp://localhost:1883` (and `MQTT_URL=tcp://localhost:1883` for the optional
  generic output).

This modifies the appliance. openWB updates should leave the installed files alone, but check
`systemctl status solix-mqtt-bridge` after each update, and reinstall after reflashing or restoring the SD card.
openWB-built devices usually give owners no shell access; run the bridge on a separate
[Raspberry Pi](raspberry-pi.md) instead.

<details>
<summary>Removing it</summary>

```sh
sudo systemctl disable --now solix-mqtt-bridge
sudo rm /etc/systemd/system/solix-mqtt-bridge.service /usr/local/bin/solix-mqtt-bridge /etc/solix-mqtt-bridge.env
sudo systemctl daemon-reload
```

The bridge publishes 0 W on shutdown, so openWB stops counting the battery. Remove the MQTT battery component in
openWB as well.

</details>

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

openWB 2.x runs on a Raspberry Pi with a regular Linux and systemd, and the bridge is a single static binary that
needs about 10 MB of RAM. It can therefore run on openWB itself and reach the broker on `localhost`.

This modifies the appliance. openWB updates should leave the files below alone, but re-check after each update, and
reinstall after reflashing or restoring the SD card. You need shell access to openWB (SSH).

1. **Pick the binary.** `uname -m` on openWB prints `aarch64` (use `linux_arm64`) or `armv7l` (use `linux_armv7`).
   The `linux_armv7` build runs in both cases.
2. **Download and install it** on openWB, replacing the version with the release you want:

   ```sh
   V=0.1.0 ARCH=linux_armv7
   curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
   curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_checksums.txt"
   sha256sum --ignore-missing -c "solix-mqtt-bridge_${V}_checksums.txt"
   tar xzf "solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
   sudo install -m 0755 solix-mqtt-bridge /usr/local/bin/solix-mqtt-bridge
   ```

   <details>
   <summary>Building it yourself instead</summary>

   Cross-compile on any machine with Go, then copy the result to openWB:

   ```sh
   CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags "-s -w" -o solix-mqtt-bridge ./cmd/solix-mqtt-bridge
   ```

   </details>

3. **Check that it reads the Solarbank** from openWB: `SOLARBANK_ADDR=<solarbank-ip>:502 solix-mqtt-bridge -probe`.
4. **Write the configuration** to `/etc/solix-mqtt-bridge.env` (readable by root only, e.g. `sudo chmod 600`):

   ```sh
   SOLARBANK_ADDR=<solarbank-ip>:502
   OPENWB_MQTT_URL=tcp://localhost:1883
   OPENWB_BAT_ID=<component id>
   # Optional: the generic output on the same broker, readable with any MQTT client.
   MQTT_URL=tcp://localhost:1883
   MQTT_BASE_TOPIC=others/solix-mqtt-bridge
   ```

5. **Install and start the service.** Copy
   [`solix-mqtt-bridge.service`](../deploy/examples/solix-mqtt-bridge.service) to `/etc/systemd/system/`, then:

   ```sh
   sudo systemctl daemon-reload
   sudo systemctl enable --now solix-mqtt-bridge
   journalctl -u solix-mqtt-bridge -f
   ```

   The unit starts the bridge at boot and restarts it after a failure. It runs as an unprivileged, sandboxed user
   (`systemd-analyze security` rates it 1.4, "OK").

**Run only one bridge per Solarbank.** Two instances use the same MQTT client ids, so the broker would keep
disconnecting one of them. Stop any other instance, such as a test run on a laptop, before starting this one.

After an openWB update, check that it is still running: `systemctl status solix-mqtt-bridge`.

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

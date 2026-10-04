# Running it on a Raspberry Pi

A dedicated Raspberry Pi on your home network is a good permanent home for the bridge: it is always on, independent of
openWB updates, and the bridge needs only about 10 MB of RAM. The bridge must reach the Solarbank (Modbus TCP, port
502) and your MQTT broker(s), for example openWB's broker on port 1883.

<details>
<summary>Recommended hardware</summary>

- Raspberry Pi 4 or 5 with 2 to 4 GB of RAM. A smaller board works for the bridge alone, but leaves no room for other
  home services.
- The official power supply. Undervoltage is the most common cause of an unstable Pi.
- A wired Ethernet connection to your router.
- A good A2 microSD card, or better an SSD (USB, or NVMe on a Pi 5), which lasts much longer under constant logging.
- A case with passive cooling.

</details>

## 1. Prepare the SD card

Use Raspberry Pi Imager on your computer:

1. **Device**: your Pi model. **Operating system**: *Raspberry Pi OS Lite (64-bit)*. **Storage**: the card or SSD.
2. Open **OS customisation** (Imager asks before writing):
   - **Hostname**, e.g. `homepi`.
   - **Username and password**: your own user; choose a strong password.
   - **Services**: enable SSH with **public-key authentication only** and paste your computer's public key. If you have
     none yet, create one with `ssh-keygen -t ed25519` and paste the content of `~/.ssh/id_ed25519.pub`.
   - **Locale**: your timezone. Leave wireless LAN empty when the Pi uses Ethernet.
3. Write the card, put it in the Pi, connect Ethernet and power.

## 2. First login

1. In your router, find the Pi and give it a fixed IP address (on a FRITZ!Box: *Heimnetz → Netzwerk*, edit the device,
   *Diesem Netzwerkgerät immer die gleiche IPv4-Adresse zuweisen*).
2. Log in from your computer and bring the system up to date:

   ```sh
   ssh <user>@<pi-ip>
   sudo apt update && sudo apt full-upgrade -y
   sudo reboot
   ```

3. Log in again and confirm the architecture: `uname -m` should print `aarch64`.

Then install the bridge with **option A** (simplest) or **option B** (if the Pi will run other containers too).

## Option A: binary and systemd

1. **Download and install the release** (replace the version with the release you want):

   ```sh
   V=0.1.0 ARCH=linux_arm64
   curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
   curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_checksums.txt"
   sha256sum --ignore-missing -c "solix-mqtt-bridge_${V}_checksums.txt"
   tar xzf "solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
   sudo install -m 0755 solix-mqtt-bridge /usr/local/bin/solix-mqtt-bridge
   ```

   The checksum line must print `OK`.

2. **Check that the Pi can read the Solarbank.** This reads once and prints JSON; compare state of charge and power
   with the Anker app:

   ```sh
   SOLARBANK_ADDR=<solarbank-ip>:502 solix-mqtt-bridge -probe
   ```

3. **Write the configuration** to `/etc/solix-mqtt-bridge.env`, e.g. with `sudo nano /etc/solix-mqtt-bridge.env`, then
   `sudo chmod 600 /etc/solix-mqtt-bridge.env`:

   ```sh
   SOLARBANK_ADDR=<solarbank-ip>:502
   # openWB output (see openwb.md for the setup in openWB)
   OPENWB_MQTT_URL=tcp://<openwb-ip>:1883
   OPENWB_BAT_ID=<component id>
   # Optional generic output; on openWB's broker it must live under others/
   MQTT_URL=tcp://<openwb-ip>:1883
   MQTT_BASE_TOPIC=others/solix-mqtt-bridge
   ```

   All settings are listed in the [README](../README.md#configuration).

4. **Install and start the service.** The unit file is in the release archive you just extracted:

   ```sh
   sudo install -m 0644 deploy/examples/solix-mqtt-bridge.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now solix-mqtt-bridge
   journalctl -u solix-mqtt-bridge -f
   ```

   Expect `mqtt connected` for each output, `connected to Solarbank`, then `Solarbank data is fresh`. Leave the log
   with Ctrl+C; the service keeps running.

systemd starts the bridge at boot and restarts it 10 s after a crash. Invalid configuration stops it instead, with the
problem shown in `systemctl status solix-mqtt-bridge`. It runs as an unprivileged, sandboxed user.

## Option B: Docker

1. **Install Docker Engine and the Compose plugin** following Docker's official installation guide for Debian;
   Raspberry Pi OS (64-bit) uses its arm64 packages.
2. **Create a directory with the example files** from the release archive (download it as in option A, step 1):

   ```sh
   mkdir -p ~/solix-mqtt-bridge
   cp deploy/examples/docker-compose.yml deploy/examples/solix-mqtt-bridge.env ~/solix-mqtt-bridge/
   cd ~/solix-mqtt-bridge
   ```

3. **Edit `solix-mqtt-bridge.env`** with the same values as in option A, step 3. In `docker-compose.yml`, pin the
   image to the release instead of `latest`, e.g. `ghcr.io/ryckakas/solix-mqtt-bridge:0.1.0`.
4. **Check the Solarbank once**, then start the service:

   ```sh
   docker run --rm -e SOLARBANK_ADDR=<solarbank-ip>:502 ghcr.io/ryckakas/solix-mqtt-bridge:0.1.0 -probe
   docker compose up -d
   docker compose logs -f
   ```

The container restarts automatically, also after a reboot of the Pi.

## Updating

- **Option A**: repeat step 1 with the new version, then `sudo systemctl restart solix-mqtt-bridge`.
- **Option B**: change the image tag in `docker-compose.yml`, then `docker compose pull && docker compose up -d`.

Check the log afterwards; `solix-mqtt-bridge starting` shows the running version.

## Only one bridge per Solarbank

Two instances use the same MQTT client ids, so the broker keeps disconnecting one of them. Stop any other instance, such
as a test run on a laptop, before starting the one on the Pi.

<details>
<summary>Troubleshooting</summary>

- **The probe fails** with a timeout or "no route to host": check that the Pi is on the same network as the Solarbank,
  that Modbus TCP is enabled in the Anker app, and that the IP address is still correct.
- **openWB shows "Fehlende MQTT-Daten"**: check `OPENWB_BAT_ID` against the topics in the component's help text, and
  that the Pi reaches openWB's broker: `nc -zv <openwb-ip> 1883`.
- **The service is `failed` with exit code 2**: the configuration is invalid; `systemctl status solix-mqtt-bridge`
  lists every problem.
- **Repeated `Solarbank read failed` warnings**: the Solarbank's Modbus server stops when the device loses its internet
  connection, and returns with it. The bridge recovers on its own; openWB sees 0 W meanwhile.

</details>

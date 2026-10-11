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

1. **Install the bridge** with the installer from the latest release:

   ```sh
   curl -fsSL https://github.com/ryckakas/solix-mqtt-bridge/releases/latest/download/install.sh | sudo sh
   ```

   It picks the build for your Pi, checks it against the release's checksums, and installs the binary to
   `/usr/local/bin`, the systemd service with its own system user `solix-mqtt-bridge`, and a configuration template
   at `/etc/solix-mqtt-bridge.env` that only root can read. It adds you to the `solix-mqtt-bridge` group, so you can
   read the log files once you log in again. It doesn't start the bridge yet, and leaves no downloads behind. To install a specific release instead,
   add its version: `... | sudo sh -s -- 0.2.3`. From another computer, run the same line through
   `ssh -t <user>@<pi-ip> '...'`.

2. **Check that the Pi can read the Solarbank.** This reads once and prints JSON; compare state of charge and power
   with the Anker app:

   ```sh
   SOLARBANK_ADDR=<solarbank-ip>:502 solix-mqtt-bridge -probe
   ```

3. **Fill in the configuration** with `sudo nano /etc/solix-mqtt-bridge.env`. Replace every placeholder, or comment
   out the lines you don't use. For the openWB output, for example:

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

4. **Start the service**, now and at every boot, and watch it connect:

   ```sh
   sudo systemctl enable --now solix-mqtt-bridge
   journalctl -u solix-mqtt-bridge -f
   ```

   Expect `mqtt connected` for each output, `connected to Solarbank`, then `Solarbank data is fresh`. Leave the log
   with Ctrl+C; the service keeps running.

systemd starts the bridge at boot and restarts it 10 s after a crash. Invalid configuration stops it instead, with the
problem shown in `systemctl status solix-mqtt-bridge`. It runs as the unprivileged, sandboxed system user
`solix-mqtt-bridge`.

<details>
<summary>Reading the installer before running it</summary>

The installer is a plain shell script. Its SHA-256 is listed in the release's checksums file.

```sh
curl -fsSLO https://github.com/ryckakas/solix-mqtt-bridge/releases/latest/download/install.sh
less install.sh
sudo sh install.sh
```

</details>

<details>
<summary>Installing without the script</summary>

Download and extract the release into its own directory (replace the version and, on a 32-bit system, the
architecture), then install the files by hand:

```sh
mkdir -p ~/solix-mqtt-bridge && cd ~/solix-mqtt-bridge
V=0.2.3 ARCH=linux_arm64
curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
curl -fsSLO "https://github.com/ryckakas/solix-mqtt-bridge/releases/download/v${V}/solix-mqtt-bridge_${V}_checksums.txt"
sha256sum --ignore-missing -c "solix-mqtt-bridge_${V}_checksums.txt"
tar xzf "solix-mqtt-bridge_${V}_${ARCH}.tar.gz"
sudo install -m 0755 solix-mqtt-bridge /usr/local/bin/solix-mqtt-bridge
sudo useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin solix-mqtt-bridge
sudo usermod -aG solix-mqtt-bridge "$USER"
sudo install -m 0644 deploy/examples/solix-mqtt-bridge.service /etc/systemd/system/
sudo install -m 0600 deploy/examples/solix-mqtt-bridge.env /etc/solix-mqtt-bridge.env
sudo systemctl daemon-reload
```

The checksum line must print `OK`. When updating, skip the `.env` line so your configuration stays. The `useradd`
and `usermod` lines are needed once; after that `useradd` only reports that the user exists. Then continue with
step 2.

</details>

**Optional log files:** add `LOG_DIR=/var/log/solix-mqtt-bridge` to the configuration and restart the service. The
bridge then writes one file per day there, including a line per poll, and deletes files older than a week
(`LOG_RETENTION_DAYS`). Members of the `solix-mqtt-bridge` group can list and open them without sudo, in any
editor. The installer adds the user who ran it with sudo; log in again once for that to apply. To add someone else:
`sudo usermod -aG solix-mqtt-bridge <user>`.

## Option B: Docker

1. **Install Docker Engine and the Compose plugin** following Docker's official installation guide for Debian;
   Raspberry Pi OS (64-bit) uses its arm64 packages.
2. **Copy the example files** out of the release archive (download and extract it as in option A,
   *Installing without the script*, up to the `tar` line):

   ```sh
   cd ~/solix-mqtt-bridge
   cp deploy/examples/docker-compose.yml deploy/examples/solix-mqtt-bridge.env .
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

**Optional log files:** create the directory for the container's user with
`mkdir logs && sudo chown 65532:65532 logs`, uncomment the `volumes` lines in `docker-compose.yml`, add
`LOG_DIR=/logs` to `solix-mqtt-bridge.env`, then `docker compose up -d`. The files appear in `~/solix-mqtt-bridge/logs`.
If the directory isn't writable, the bridge keeps running and says so in `docker compose logs`.

## Updating

- **Option A**: run the installer from step 1 again. It installs the new binary, and the service file if it changed,
  keeps your configuration, and restarts the bridge if it's enabled. When nothing changed it leaves the bridge alone.
  To go back to an older release, run it with that version: `... | sudo sh -s -- 0.2.2`.
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
  that the Pi reaches openWB's broker: `nc -zv <openwb-ip> 1883`. If it doesn't, the log shows
  `mqtt connect failed; retrying` with the reason: a refused connection or a timeout, or "bad user name or password"
  or "not Authorized" for a rejected login.
- **The service is `failed` with exit code 2**: the configuration is invalid; `systemctl status solix-mqtt-bridge`
  lists every problem.
- **Repeated `Solarbank read failed` warnings**: the Solarbank's Modbus server stops when the device loses its internet
  connection, and returns with it. The bridge recovers on its own; openWB sees 0 W meanwhile.

</details>

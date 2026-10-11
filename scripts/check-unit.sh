#!/bin/sh
# Checks the systemd unit with systemd's own analyzer, in a throwaway Debian container (make unit-check). It creates
# the user, binary and config file the unit refers to, since verify checks that they exist.
set -eu

unit=deploy/examples/solix-mqtt-bridge.service
# The unit's exposure score in tenths (1.2 today), so any weakening of the sandbox fails.
threshold=12

apt-get update -qq
apt-get install -qq -y --no-install-recommends systemd >/dev/null
useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin solix-mqtt-bridge
printf '#!/bin/sh\n' >/usr/local/bin/solix-mqtt-bridge
chmod 0755 /usr/local/bin/solix-mqtt-bridge
touch /etc/solix-mqtt-bridge.env

out=$(systemd-analyze verify "$unit" 2>&1) || {
	echo "$out"
	exit 1
}
if [ -n "$out" ]; then
	echo "$out"
	echo "systemd-analyze verify reported problems in $unit"
	exit 1
fi
echo "systemd-analyze verify: $unit OK"

status=0
out=$(systemd-analyze security --offline=true --threshold="$threshold" "$unit" 2>&1) || status=$?
if [ "$status" -ne 0 ]; then
	echo "$out"
	echo "the unit's exposure is above the threshold of $threshold (tenths): its sandbox got weaker"
	exit "$status"
fi
echo "$out" | tail -n 1

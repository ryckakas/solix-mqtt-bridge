// Command solix-openwb-bridge publishes an Anker SOLIX Solarbank's battery data, read over local Modbus TCP, to an
// openWB 2.x wallbox over MQTT.
package main

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	fmt.Printf("solix-openwb-bridge %s (%s, %s)\n", version, commit, date)
}

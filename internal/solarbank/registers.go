// Package solarbank reads an Anker SOLIX Solarbank Max AC (model A17E2) over Modbus TCP, using input-register reads
// (function code 04) only.
package solarbank

// Derived from Anker's official integration (MIT, see THIRD_PARTY_NOTICES): anker-charging/ha-anker-solix-official
// @654bd167544fc81a1e81cb4b001504c8b102edf0, custom_components/anker_solix_official/config/
// 8fcbb87c685781b1d70d784a79eb923098955df2aaf199095ce7767bb70b913d.yaml

type block struct {
	start uint16
	count uint16
}

// Boundaries mirror the ranges Anker's integration polls, which are proven against real devices.
var (
	liveBlock     = block{start: 10000, count: 51}
	batteryBlock  = block{start: 10208, count: 58}
	identityBlock = block{start: 10090, count: 67}
	modelBlock    = block{start: 32768, count: 7}
)

const (
	regStatus            = 10001
	regPVPower           = 10002
	regThirdPartyPVPower = 10004
	regBatteryPower      = 10008
	regHomeLoad          = 10010
	regGridPower         = 10012
	regSoC               = 10014
	regMaxChargePower    = 10036
	regMaxDischargePower = 10038

	regSerial   = 10100
	regFirmware = 10112

	regCapacity        = 10250
	regSectionStatus   = 10252
	regSectionSoC      = 10256
	regChargedTotal    = 10262
	regDischargedTotal = 10264

	regModel = 32768

	serialWords   = 12
	firmwareWords = 6
	modelWords    = 5

	whPerEnergyUnit = 100
)

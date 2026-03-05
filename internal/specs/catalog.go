package specs

// epcCatalog is derived from pychonet's EPC catalog for selected classes.
// Used as fallback for metric help text when help is omitted in YAML.
var epcCatalog = map[[2]byte]map[byte]string{
	{0x01, 0x30}: {
		0x80: "Operation status", 0x8F: "Operation power-saving", 0xA0: "Air flow rate setting",
		0xA1: "Automatic control of air flow direction setting", 0xA3: "Automatic swing of air flow setting",
		0xA4: "Air flow direction (vertical) setting", 0xA5: "Air flow direction (horizontal) setting",
		0xAA: "Special state", 0xB0: "Operation mode setting", 0xB1: "Automatic temperature control setting",
		0xB2: "Normal/high-speed/silent operation setting", 0xB3: "Set temperature value",
		0xB4: "Set value of relative humidity in dehumidifying mode", 0xB8: "Rated power consumption",
		0xB9: "Measured value of current consumption", 0xBA: "Measured value of room relative humidity",
		0xBB: "Measured value of room temperature", 0xBC: "Set temperature value of user remote control",
		0xBD: "Measured cooled air temperature", 0xBE: "Measured outdoor air temperature",
		0xBF: "Relative temperature setting", 0xC0: "Ventilation function setting",
		0xC1: "Humidifier function setting", 0xC2: "Ventilation air flow rate setting",
		0xC4: "Degree of humidification setting", 0xCC: "Special function setting",
		0xCF: "Air purification mode setting",
	},
	{0x02, 0x79}: {
		0x80: "Operation status", 0xA0: "Output power control setting 1", 0xA1: "Output power control setting 2",
		0xE0: "Measured instantaneous amount of electricity generated",
		0xE1: "Measured cumulative amount of electric energy generated",
		0xE2: "Resetting cumulative amount of electric energy generated",
		0xE3: "Measured cumulative amount of electric energy sold",
		0xE4: "Resetting cumulative amount of electric energy sold",
		0xE5: "Power generation output limit setting 1", 0xE6: "Power generation output limit setting 2",
		0xE7: "Limit setting for the amount of electricity sold",
		0xE8: "Rated power generation output (System-interconnected)",
		0xE9: "Rated power generation output (Independent)",
	},
	{0x02, 0x7D}: {
		0x80: "Operation status", 0x83: "Identification number", 0x97: "Current time setting",
		0x98: "Current date setting", 0xA0: "AC effective capacity (charging)", 0xA1: "AC effective capacity (discharging)",
		0xA2: "AC chargeable capacity", 0xA3: "AC dischargeable capacity", 0xA4: "AC chargeable electric energy",
		0xA5: "AC dischargeable electric energy", 0xA6: "AC charge upper limit setting", 0xA7: "AC discharge lower limit setting",
		0xA8: "AC measured cumulative charging electric energy", 0xA9: "AC measured cumulative discharging electric energy",
		0xAA: "AC charge amount setting value", 0xAB: "AC discharge amount setting value",
		0xC1: "Charging method", 0xC2: "Discharging method", 0xC7: "AC rated electric energy",
		0xC8: "Minimum/maximum charging electric power", 0xC9: "Minimum/maximum discharging electric power",
		0xCA: "Minimum/maximum charging current", 0xCB: "Minimum/maximum discharging current",
		0xCC: "Re-interconnection permission setting", 0xCD: "Operation permission setting",
		0xCE: "Independent operation permission setting", 0xCF: "Working operation status",
		0xD0: "Rated electric energy", 0xD1: "Rated capacity", 0xD2: "Rated voltage",
		0xD3: "Measured instantaneous charging/discharging electric energy",
		0xD4: "Measured instantaneous charging/discharging current",
		0xD5: "Measured instantaneous charging/discharging voltage",
		0xD6: "Measured cumulative discharging electric energy", 0xD8: "Measured cumulative charging electric energy",
		0xDA: "Operation mode setting", 0xDB: "System-interconnected type",
		0xE0: "Charging/discharging amount setting 1", 0xE1: "Charging/discharging amount setting 2",
		0xE2: "Remaining stored electricity 1", 0xE3: "Remaining stored electricity 2",
		0xE4: "Remaining stored electricity 3", 0xE5: "Battery state of health", 0xE6: "Battery type",
		0xE7: "Charging amount setting 1", 0xE8: "Discharging amount setting 1",
		0xE9: "Charging amount setting 2", 0xEA: "Discharging amount setting 2",
		0xEB: "Charging electric energy setting", 0xEC: "Discharging electric energy setting",
		0xED: "Charging current setting", 0xEE: "Discharging current setting",
		0xEF: "Rated voltage (Independent)",
	},
}

// LookupEPCName returns a human-readable name for an EPC in the given EOJ class.
func LookupEPCName(eoj [3]byte, epc byte) string {
	classKey := [2]byte{eoj[0], eoj[1]}
	epcs, ok := epcCatalog[classKey]
	if !ok {
		return ""
	}
	return epcs[epc]
}

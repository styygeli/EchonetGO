package specs

import "time"

// Super Class EPC ranges per ECHONET Lite: 0x80–0x8F (common object properties),
// 0x9D–0x9F (property maps). 0x9E/0x9F are used at runtime for GETMAP only and
// are not exposed as sensor metrics.

// SuperClassMetrics returns the canonical Super Class metric defaults that apply
// to all device objects. Used by the loader to merge into each class spec;
// class YAML definitions override these for the same EPC.
func SuperClassMetrics() []MetricSpec {
	opEnum := map[int]string{0x30: "on", 0x31: "off"}
	opReverse := map[string]int{"on": 0x30, "off": 0x31}
	faultEnum := map[int]string{0x41: "fault", 0x42: "no_fault"}
	faultReverse := map[string]int{"fault": 0x41, "no_fault": 0x42}

	return []MetricSpec{
		{
			EPC:            0x80,
			Name:           "operation_status",
			Help:           "Operation status: 0x30=ON, 0x31=OFF.",
			Size:           1,
			Scale:          1,
			Type:           "gauge",
			Enum:           opEnum,
			ReverseEnum:    opReverse,
			ScrapeInterval: 0, // use device default
		},
		{
			EPC:            0x88,
			Name:           "fault_status",
			Help:           "Fault status.",
			Size:           1,
			Scale:          1,
			Type:           "gauge",
			Enum:           faultEnum,
			ReverseEnum:    faultReverse,
			ScrapeInterval: 10 * time.Minute,
		},
	}
}

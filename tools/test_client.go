package main

import (
	"fmt"

	"github.com/styygeli/echonetgo/internal/echonet"
)

func main() {
	devices := []struct {
		name string
		ip   string
		eoj  [3]byte
	}{
		{"epcube_battery", "192.168.3.249", [3]byte{0x02, 0x7D, 0x01}},
		{"epcube_solar", "192.168.3.249", [3]byte{0x02, 0x79, 0x01}},
		{"ac_mbr", "192.168.3.251", [3]byte{0x01, 0x30, 0x01}},
		{"ac_av", "192.168.3.250", [3]byte{0x01, 0x30, 0x01}},
		{"ac_house", "192.168.0.249", [3]byte{0x01, 0x30, 0x01}},
	}

	client := echonet.NewClient(3, false) // 3 sec timeout, strictSourcePort3610 = false

	for _, d := range devices {
		fmt.Printf("\nTesting %s (%s) ...\n", d.name, d.ip)

		info, err := client.GetDeviceInfo(d.ip, d.eoj)
		if err != nil {
			fmt.Printf("GetDeviceInfo failed: %v\n", err)
		} else {
			fmt.Printf("DeviceInfo: %+v\n", info)
		}

		props, err := client.GetProps(d.ip, d.eoj, []byte{0x80}) // Operation status
		if err != nil {
			fmt.Printf("GetProps(0x80) failed: %v\n", err)
		} else {
			fmt.Printf("GetProps(0x80) returned %d props\n", len(props))
			for _, p := range props {
				fmt.Printf("  EPC: %02x, EDT: %x\n", p.EPC, p.EDT)
			}
		}

		readable, err := client.GetReadablePropertyMap(d.ip, d.eoj)
		if err != nil {
			fmt.Printf("GetReadablePropertyMap failed: %v\n", err)
		} else {
			var epcs []byte
			for epc := range readable {
				epcs = append(epcs, epc)
			}
			fmt.Printf("Readable Property Map: %x\n", epcs)
		}
	}
}

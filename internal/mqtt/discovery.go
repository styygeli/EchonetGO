package mqtt

import (
	"encoding/json"
	"fmt"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/specs"
)

// haDiscoveryPayload is the JSON structure for HA MQTT sensor auto-discovery.
type haDiscoveryPayload struct {
	Name                      string   `json:"name"`
	UniqueID                  string   `json:"unique_id"`
	StateTopic                string   `json:"state_topic"`
	ValueTemplate             string   `json:"value_template"`
	DeviceClass               string   `json:"device_class,omitempty"`
	StateClass                string   `json:"state_class,omitempty"`
	UnitOfMeasurement         string   `json:"unit_of_measurement,omitempty"`
	Options                   []string `json:"options,omitempty"`
	AvailabilityTopic         string   `json:"availability_topic"`
	ExpireAfter               int      `json:"expire_after"`
	ForceUpdate               bool     `json:"force_update"`
	SuggestedDisplayPrecision *int     `json:"suggested_display_precision,omitempty"`
	Device                    haDevice `json:"device"`
}

// haClimateDiscoveryPayload is the JSON structure for HA MQTT climate auto-discovery.
type haClimateDiscoveryPayload struct {
	Name                    string   `json:"name"`
	UniqueID                string   `json:"unique_id"`
	ModeCommandTopic        string   `json:"mode_command_topic"`
	ModeStateTopic          string   `json:"mode_state_topic"`
	TemperatureCommandTopic string   `json:"temperature_command_topic"`
	TemperatureStateTopic   string   `json:"temperature_state_topic"`
	CurrentTemperatureTopic string   `json:"current_temperature_topic"`
	PowerCommandTopic       string   `json:"power_command_topic"`
	FanModeCommandTopic     string   `json:"fan_mode_command_topic,omitempty"`
	FanModeStateTopic       string   `json:"fan_mode_state_topic,omitempty"`
	MinTemp                 float64  `json:"min_temp"`
	MaxTemp                 float64  `json:"max_temp"`
	TempStep                float64  `json:"temp_step"`
	Precision               float64  `json:"precision"`
	AvailabilityTopic       string   `json:"availability_topic"`
	ExpireAfter             int      `json:"expire_after"`
	Device                  haDevice `json:"device"`
	Modes                   []string `json:"modes"`
	FanModes                []string `json:"fan_modes,omitempty"`
}

type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

func (p *Publisher) ensureDiscovery(dev config.Device, info echonet.DeviceInfo, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := dev.Name
	infoKey := info.Manufacturer + "|" + info.ProductCode
	if prev, ok := p.published[key]; ok && prev == infoKey {
		return
	}

	device := haDevice{
		Identifiers:  []string{"echonetgo_" + dev.Name},
		Name:         friendlyName(dev.Name),
		Manufacturer: info.Manufacturer,
		Model:        info.ProductCode,
		SWVersion:    p.swVersion,
		ViaDevice:    "echonetgo",
	}
	if info.UID != "" {
		device.Identifiers = append(device.Identifiers, info.UID+"_"+dev.Name)
	}

	availTopic := fmt.Sprintf("%s/%s/availability", p.topicPrefix, dev.Name)
	stateTopic := fmt.Sprintf("%s/%s/state", p.topicPrefix, dev.Name)

	for _, ms := range metricSpecs {
		objectID := dev.Name + "_" + ms.Name
		configTopic := fmt.Sprintf("%s/sensor/%s/config", p.discoveryPrefix, objectID)

		payload := haDiscoveryPayload{
			Name:              friendlyName(ms.Name),
			UniqueID:          "echonetgo_" + objectID,
			StateTopic:        stateTopic,
			ValueTemplate:     fmt.Sprintf("{{ value_json.%s | default(None) }}", ms.Name),
			AvailabilityTopic: availTopic,
			ExpireAfter:       300,
			Device:            device,
			ForceUpdate:       true,
		}

		if ms.HADeviceClass != "" && ms.HADeviceClass != "enum" {
			payload.DeviceClass = ms.HADeviceClass
		}
		if ms.HAStateClass != "" {
			payload.StateClass = ms.HAStateClass
		}
		if ms.HAUnit != "" {
			payload.UnitOfMeasurement = ms.HAUnit
		}

		if ms.HADeviceClass == "enum" && len(ms.Enum) > 0 {
			payload.DeviceClass = "enum"
			payload.StateClass = ""
			payload.UnitOfMeasurement = ""
			options := make([]string, 0, len(ms.Enum))
			for _, label := range ms.Enum {
				options = append(options, label)
			}
			payload.Options = options
			payload.ValueTemplate = fmt.Sprintf("{{ value_json.%s_str | default(value_json.%s | default(None)) }}", ms.Name, ms.Name)
		}

		if ms.HADeviceClass == "power" || ms.HADeviceClass == "energy" {
			payload.SuggestedDisplayPrecision = intPtr(1)
		}
		if ms.HADeviceClass == "temperature" {
			payload.SuggestedDisplayPrecision = intPtr(1)
		}

		data, err := json.Marshal(payload)
		if err != nil {
			mqttLog.Warnf("marshal discovery for %s/%s: %v", dev.Name, ms.Name, err)
			continue
		}
		token := p.client.Publish(configTopic, qos, true, data)
		if !token.WaitTimeout(publishTimeout) {
			mqttLog.Warnf("publish discovery timeout for %s/%s", dev.Name, ms.Name)
		} else if err := token.Error(); err != nil {
			mqttLog.Warnf("publish discovery for %s/%s: %v", dev.Name, ms.Name, err)
		}
	}
	if writable != nil {
		p.publishWritableDiscovery(dev, device, availTopic, metricSpecs, writable, climateSpec)
	}
	if climateSpec != nil {
		p.publishClimateDiscovery(dev, device, availTopic, climateSpec, metricSpecs)
	}
	p.published[key] = infoKey
	mqttLog.Infof("published discovery for %s (%d sensors, mfg=%q model=%q)", dev.Name, len(metricSpecs), info.Manufacturer, info.ProductCode)
}

func (p *Publisher) publishClimateDiscovery(dev config.Device, device haDevice, availTopic string, cl *specs.ClimateSpec, metricSpecs []specs.MetricSpec) {
	base := fmt.Sprintf("%s/%s/climate", p.topicPrefix, dev.Name)
	modeState := base + "/mode/state"
	modeCmd := base + "/mode/set"
	tempState := base + "/temperature/state"
	tempCmd := base + "/temperature/set"
	currentTemp := base + "/current_temperature"
	powerCmd := base + "/power/set"

	payload := haClimateDiscoveryPayload{
		Name:                    friendlyName(dev.Name),
		UniqueID:                "echonetgo_" + dev.Name + "_climate",
		ModeCommandTopic:        modeCmd,
		ModeStateTopic:          modeState,
		TemperatureCommandTopic: tempCmd,
		TemperatureStateTopic:   tempState,
		CurrentTemperatureTopic: currentTemp,
		PowerCommandTopic:       powerCmd,
		MinTemp:                 cl.MinTemp,
		MaxTemp:                 cl.MaxTemp,
		TempStep:                cl.TempStep,
		Precision:               1.0,
		AvailabilityTopic:       availTopic,
		ExpireAfter:             300,
		Device:                  device,
		Modes:                   climateModesList(cl.Modes),
	}
	if cl.FanModeEPC != 0 {
		payload.FanModeCommandTopic = base + "/fan_mode/set"
		payload.FanModeStateTopic = base + "/fan_mode/state"
		payload.FanModes = fanModesFromSpec(metricSpecs, cl.FanModeEPC)
		if len(payload.FanModes) == 0 {
			payload.FanModes = []string{"auto", "low", "medium", "high"}
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		mqttLog.Warnf("marshal climate discovery for %s: %v", dev.Name, err)
		return
	}
	configTopic := fmt.Sprintf("%s/climate/%s_climate/config", p.discoveryPrefix, dev.Name)
	token := p.client.Publish(configTopic, qos, true, data)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish climate discovery timeout for %s", dev.Name)
		return
	}
	if err := token.Error(); err != nil {
		mqttLog.Warnf("publish climate discovery for %s: %v", dev.Name, err)
		return
	}
	mqttLog.Infof("published climate discovery for %s", dev.Name)
}

func (p *Publisher) publishWritableDiscovery(dev config.Device, device haDevice, availTopic string, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec) {
	for _, ms := range metricSpecs {
		if _, ok := writable[ms.EPC]; !ok {
			continue
		}
		if isClimateEPC(ms.EPC, climateSpec) {
			continue
		}
		entityType := writableEntityType(ms)
		if entityType == "" {
			continue
		}
		objectID := dev.Name + "_" + ms.Name
		base := fmt.Sprintf("%s/%s/%s/%s", p.topicPrefix, dev.Name, entityType, ms.Name)
		stateTopic := base + "/state"
		commandTopic := base + "/set"
		switch entityType {
		case "switch":
			payload := map[string]any{
				"name":               friendlyName(ms.Name),
				"unique_id":          "echonetgo_" + objectID,
				"command_topic":      commandTopic,
				"state_topic":        stateTopic,
				"availability_topic": availTopic,
				"expire_after":       300,
				"device":             device,
			}
			data, _ := json.Marshal(payload)
			token := p.client.Publish(fmt.Sprintf("%s/switch/%s/config", p.discoveryPrefix, objectID), qos, true, data)
			if token.WaitTimeout(publishTimeout) && token.Error() == nil {
				mqttLog.Debugf("published switch discovery for %s/%s", dev.Name, ms.Name)
			}
		case "select":
			options := make([]string, 0, len(ms.ReverseEnum))
			for label := range ms.ReverseEnum {
				options = append(options, label)
			}
			sortStrings(options)
			payload := map[string]any{
				"name":               friendlyName(ms.Name),
				"unique_id":          "echonetgo_" + objectID,
				"command_topic":      commandTopic,
				"state_topic":        stateTopic,
				"options":            options,
				"availability_topic": availTopic,
				"expire_after":       300,
				"device":             device,
			}
			data, _ := json.Marshal(payload)
			token := p.client.Publish(fmt.Sprintf("%s/select/%s/config", p.discoveryPrefix, objectID), qos, true, data)
			if token.WaitTimeout(publishTimeout) && token.Error() == nil {
				mqttLog.Debugf("published select discovery for %s/%s", dev.Name, ms.Name)
			}
		case "number":
			minVal, maxVal := 0.0, 100.0
			if ms.Scale != 0 {
				maxVal = 100
			}
			step := 1.0
			if ms.Scale != 0 {
				step = ms.Scale
			}
			payload := map[string]any{
				"name":               friendlyName(ms.Name),
				"unique_id":          "echonetgo_" + objectID,
				"command_topic":      commandTopic,
				"state_topic":        stateTopic,
				"min":                minVal,
				"max":                maxVal,
				"step":               step,
				"availability_topic": availTopic,
				"expire_after":       300,
				"device":             device,
			}
			if ms.HAUnit != "" {
				payload["unit_of_measurement"] = ms.HAUnit
			}
			data, _ := json.Marshal(payload)
			token := p.client.Publish(fmt.Sprintf("%s/number/%s/config", p.discoveryPrefix, objectID), qos, true, data)
			if token.WaitTimeout(publishTimeout) && token.Error() == nil {
				mqttLog.Debugf("published number discovery for %s/%s", dev.Name, ms.Name)
			}
		}
	}
}

// publishBridgeDevice registers the EchonetGO bridge as a named device in HA
// so that child devices (via_device) have a proper parent.
func (p *Publisher) publishBridgeDevice() {
	device := haDevice{
		Identifiers:  []string{"echonetgo"},
		Name:         "EchonetGO",
		Manufacturer: "github.com/styygeli/EchonetGO",
		Model:        "ECHONET Lite Gateway",
		SWVersion:    p.swVersion,
	}
	availTopic := fmt.Sprintf("%s/bridge/availability", p.topicPrefix)
	stateTopic := fmt.Sprintf("%s/bridge/state", p.topicPrefix)

	payload := haDiscoveryPayload{
		Name:              "Status",
		UniqueID:          "echonetgo_bridge_status",
		StateTopic:        stateTopic,
		ValueTemplate:     "{{ value_json.status }}",
		AvailabilityTopic: availTopic,
		Device:            device,
		ForceUpdate:       true,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		mqttLog.Warnf("marshal bridge discovery: %v", err)
		return
	}
	token := p.client.Publish(fmt.Sprintf("%s/sensor/echonetgo_bridge_status/config", p.discoveryPrefix), qos, true, data)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish bridge discovery timeout")
		return
	}

	p.client.Publish(availTopic, qos, true, "online")
	stateData, _ := json.Marshal(map[string]string{"status": "online"})
	p.client.Publish(stateTopic, qos, true, stateData)

	mqttLog.Infof("published bridge device discovery")
}

var titleCaser = cases.Title(language.English)

func friendlyName(name string) string {
	return strings.ReplaceAll(titleCaser.String(strings.ReplaceAll(name, "_", " ")), "  ", " ")
}

func intPtr(v int) *int { return &v }

func fanModesFromSpec(metricSpecs []specs.MetricSpec, epc byte) []string {
	for _, m := range metricSpecs {
		if m.EPC != epc {
			continue
		}
		if len(m.ReverseEnum) == 0 {
			return nil
		}
		order := []string{"auto", "level_1", "level_2", "level_3", "level_4", "level_5", "level_6", "level_7", "level_8"}
		out := make([]string, 0, len(m.ReverseEnum))
		for _, label := range order {
			if _, ok := m.ReverseEnum[label]; ok {
				out = append(out, label)
			}
		}
		for label := range m.ReverseEnum {
			found := false
			for _, o := range order {
				if o == label {
					found = true
					break
				}
			}
			if !found {
				out = append(out, label)
			}
		}
		return out
	}
	return nil
}

func climateModesList(modes map[string]*int) []string {
	order := []string{"off", "auto", "cool", "heat", "dry", "fan_only"}
	out := make([]string, 0, len(modes))
	for _, m := range order {
		if _, ok := modes[m]; ok {
			out = append(out, m)
		}
	}
	for m := range modes {
		found := false
		for _, o := range order {
			if o == m {
				found = true
				break
			}
		}
		if !found {
			out = append(out, m)
		}
	}
	return out
}

func metricNameForEPC(specs []specs.MetricSpec, epc byte) string {
	if epc == 0 {
		return ""
	}
	for _, m := range specs {
		if m.EPC == epc {
			return m.Name
		}
	}
	return ""
}

func isClimateEPC(epc byte, cl *specs.ClimateSpec) bool {
	if cl == nil {
		return false
	}
	if epc == 0x80 {
		return true
	}
	if epc == cl.ModeEPC || epc == cl.TemperatureEPC || epc == cl.CurrentTemperatureEPC || epc == cl.FanModeEPC {
		return true
	}
	return false
}

// writableEntityType returns "switch", "select", or "number" for a writable metric; "" if not applicable.
func writableEntityType(ms specs.MetricSpec) string {
	if ms.ExcludeSet {
		return ""
	}
	if len(ms.Enum) == 2 {
		var hasOn, hasOff bool
		for _, label := range ms.Enum {
			switch strings.ToLower(label) {
			case "on":
				hasOn = true
			case "off":
				hasOff = true
			}
		}
		if hasOn && hasOff {
			return "switch"
		}
		return "select"
	}
	if len(ms.Enum) > 2 {
		return "select"
	}
	if len(ms.Enum) == 0 {
		return "number"
	}
	return ""
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[i] > s[j] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

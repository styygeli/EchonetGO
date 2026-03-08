// Package mqtt publishes ECHONET device state to an MQTT broker using
// Home Assistant's MQTT auto-discovery protocol.
package mqtt

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/specs"
)

var mqttLog = logging.New("mqtt")

const (
	connectTimeout = 10 * time.Second
	publishTimeout = 5 * time.Second
	qos            = 1
)

// Publisher handles MQTT connection and publishes HA discovery + state.
type Publisher struct {
	client          pahomqtt.Client
	topicPrefix     string
	discoveryPrefix string
	swVersion       string

	mu        sync.Mutex
	published map[string]string // tracks device name -> "manufacturer|model" published
}

// NewPublisher creates a connected MQTT publisher. Returns nil if broker is empty.
func NewPublisher(cfg config.MQTTConfig, swVersion string) (*Publisher, error) {
	if cfg.Broker == "" {
		return nil, nil
	}
	opts := pahomqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID("echonetgo").
		SetKeepAlive(60 * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10 * time.Second).
		SetCleanSession(true).
		SetConnectionLostHandler(func(_ pahomqtt.Client, err error) {
			mqttLog.Warnf("connection lost: %v", err)
		}).
		SetOnConnectHandler(func(_ pahomqtt.Client) {
			mqttLog.Infof("connected to %s", cfg.Broker)
		})
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
	}
	if cfg.Password != "" {
		opts.SetPassword(cfg.Password)
	}

	client := pahomqtt.NewClient(opts)
	token := client.Connect()
	if !token.WaitTimeout(connectTimeout) {
		return nil, fmt.Errorf("mqtt connect timeout to %s", cfg.Broker)
	}
	if err := token.Error(); err != nil {
		return nil, fmt.Errorf("mqtt connect to %s: %w", cfg.Broker, err)
	}

	pub := &Publisher{
		client:          client,
		topicPrefix:     cfg.TopicPrefix,
		discoveryPrefix: cfg.DiscoveryPrefix,
		swVersion:       swVersion,
		published:       make(map[string]string),
	}
	pub.publishBridgeDevice()
	return pub, nil
}

// Disconnect cleanly shuts down the MQTT connection.
func (p *Publisher) Disconnect() {
	topic := fmt.Sprintf("%s/bridge/availability", p.topicPrefix)
	token := p.client.Publish(topic, qos, true, "offline")
	token.WaitTimeout(publishTimeout)
	p.client.Disconnect(1000)
	mqttLog.Infof("disconnected")
}

// PublishDeviceState publishes state for a device and ensures discovery has been sent.
func (p *Publisher) PublishDeviceState(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, success bool) {
	p.ensureDiscovery(dev, info, metricSpecs)
	p.publishAvailability(dev, success)
	if success && len(metrics) > 0 {
		p.publishState(dev, metrics)
	}
}

func (p *Publisher) ensureDiscovery(dev config.Device, info echonet.DeviceInfo, metricSpecs []specs.MetricSpec) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := dev.Name
	infoKey := info.Manufacturer + "|" + info.ProductCode
	if prev, ok := p.published[key]; ok && prev == infoKey {
		return
	}

	device := haDevice{
		Identifiers:  []string{"echonetgo_" + dev.Name},
		Name:         friendlyDeviceName(dev.Name),
		Manufacturer: info.Manufacturer,
		Model:        info.ProductCode,
		SWVersion:    p.swVersion,
		ViaDevice:    "echonetgo",
	}
	if info.UID != "" {
		device.Identifiers = append(device.Identifiers, info.UID)
	}

	availTopic := fmt.Sprintf("%s/%s/availability", p.topicPrefix, dev.Name)
	stateTopic := fmt.Sprintf("%s/%s/state", p.topicPrefix, dev.Name)

	for _, ms := range metricSpecs {
		objectID := dev.Name + "_" + ms.Name
		configTopic := fmt.Sprintf("%s/sensor/%s/config", p.discoveryPrefix, objectID)

		payload := haDiscoveryPayload{
			Name:              friendlyMetricName(ms.Name),
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
	p.published[key] = infoKey
	mqttLog.Infof("published discovery for %s (%d sensors, mfg=%q model=%q)", dev.Name, len(metricSpecs), info.Manufacturer, info.ProductCode)
}

func (p *Publisher) publishState(dev config.Device, metrics map[string]echonet.MetricValue) {
	stateTopic := fmt.Sprintf("%s/%s/state", p.topicPrefix, dev.Name)
	payload := make(map[string]any, len(metrics))
	for name, mv := range metrics {
		payload[name] = mv.Value
		if mv.EnumLabel != "" {
			payload[name+"_str"] = mv.EnumLabel
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		mqttLog.Warnf("marshal state for %s: %v", dev.Name, err)
		return
	}
	token := p.client.Publish(stateTopic, qos, false, data)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish state timeout for %s", dev.Name)
	}
}

func (p *Publisher) publishAvailability(dev config.Device, online bool) {
	topic := fmt.Sprintf("%s/%s/availability", p.topicPrefix, dev.Name)
	payload := "offline"
	if online {
		payload = "online"
	}
	token := p.client.Publish(topic, qos, true, payload)
	if !token.WaitTimeout(publishTimeout) {
		mqttLog.Warnf("publish availability timeout for %s", dev.Name)
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
		ExpireAfter:       300,
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

	// Publish availability and initial state.
	p.client.Publish(availTopic, qos, true, "online")
	stateData, _ := json.Marshal(map[string]string{"status": "online"})
	p.client.Publish(stateTopic, qos, true, stateData)

	mqttLog.Infof("published bridge device discovery")
}

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

type haDevice struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Model        string   `json:"model,omitempty"`
	SWVersion    string   `json:"sw_version,omitempty"`
	ViaDevice    string   `json:"via_device,omitempty"`
}

var titleCaser = cases.Title(language.English)

func friendlyDeviceName(name string) string {
	return strings.ReplaceAll(titleCaser.String(strings.ReplaceAll(name, "_", " ")), "  ", " ")
}

func friendlyMetricName(name string) string {
	return strings.ReplaceAll(titleCaser.String(strings.ReplaceAll(name, "_", " ")), "  ", " ")
}

func intPtr(v int) *int { return &v }

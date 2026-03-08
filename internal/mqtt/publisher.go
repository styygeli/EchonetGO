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

// Client returns the MQTT client for subscriptions (e.g. Commander).
func (p *Publisher) Client() pahomqtt.Client {
	return p.client
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
func (p *Publisher) PublishDeviceState(dev config.Device, info echonet.DeviceInfo, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec, success bool) {
	p.ensureDiscovery(dev, info, metricSpecs, writable, climateSpec)
	p.publishAvailability(dev, success)
	if success && len(metrics) > 0 {
		p.publishState(dev, metrics)
		if climateSpec != nil {
			p.publishClimateState(dev, metrics, metricSpecs, climateSpec)
		}
		if writable != nil {
			p.publishWritableState(dev, metrics, metricSpecs, writable, climateSpec)
		}
	}
}

func (p *Publisher) publishClimateState(dev config.Device, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, cl *specs.ClimateSpec) {
	base := fmt.Sprintf("%s/%s/climate", p.topicPrefix, dev.Name)
	modeName := metricNameForEPC(metricSpecs, cl.ModeEPC)
	tempName := metricNameForEPC(metricSpecs, cl.TemperatureEPC)
	currentName := metricNameForEPC(metricSpecs, cl.CurrentTemperatureEPC)
	fanName := metricNameForEPC(metricSpecs, cl.FanModeEPC)

	operationStatusName := metricNameForEPC(metricSpecs, 0x80)
	var modeStr string
	if operationStatusName != "" {
		if mv, ok := metrics[operationStatusName]; ok {
			if int(mv.Value) == 0x31 {
				modeStr = "off"
			}
		}
	}
	if modeStr != "off" && modeName != "" {
		if mv, ok := metrics[modeName]; ok {
			raw := int(mv.Value)
			for label, v := range cl.Modes {
				if v != nil && *v == raw {
					modeStr = label
					break
				}
			}
			if modeStr == "" {
				modeStr = "heat"
			}
		}
	}
	if modeStr != "" {
		p.client.Publish(base+"/mode/state", qos, false, modeStr)
	}
	if tempName != "" {
		if mv, ok := metrics[tempName]; ok {
			p.client.Publish(base+"/temperature/state", qos, false, fmt.Sprintf("%.0f", mv.Value))
		}
	}
	if currentName != "" {
		if mv, ok := metrics[currentName]; ok {
			p.client.Publish(base+"/current_temperature", qos, false, fmt.Sprintf("%.0f", mv.Value))
		}
	}
	if fanName != "" && cl.FanModeEPC != 0 {
		if mv, ok := metrics[fanName]; ok && mv.EnumLabel != "" {
			p.client.Publish(base+"/fan_mode/state", qos, false, mv.EnumLabel)
		}
	}
}

func (p *Publisher) publishWritableState(dev config.Device, metrics map[string]echonet.MetricValue, metricSpecs []specs.MetricSpec, writable map[byte]struct{}, climateSpec *specs.ClimateSpec) {
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
		mv, ok := metrics[ms.Name]
		if !ok {
			continue
		}
		base := fmt.Sprintf("%s/%s/%s/%s", p.topicPrefix, dev.Name, entityType, ms.Name)
		stateTopic := base + "/state"
		var payload string
		switch entityType {
		case "switch":
			if mv.EnumLabel != "" {
				switch strings.ToLower(mv.EnumLabel) {
				case "on":
					payload = "ON"
				case "off":
					payload = "OFF"
				default:
					payload = mv.EnumLabel
				}
			} else if mv.Value != 0 {
				payload = "ON"
			} else {
				payload = "OFF"
			}
		case "select":
			if mv.EnumLabel != "" {
				payload = mv.EnumLabel
			} else {
				payload = fmt.Sprintf("%.0f", mv.Value)
			}
		case "number":
			payload = fmt.Sprintf("%v", mv.Value)
		default:
			continue
		}
		p.client.Publish(stateTopic, qos, false, payload)
	}
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

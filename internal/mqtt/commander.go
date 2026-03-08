package mqtt

import (
	"context"
	"strconv"
	"strings"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"

	"github.com/styygeli/echonetgo/internal/config"
	"github.com/styygeli/echonetgo/internal/echonet"
	"github.com/styygeli/echonetgo/internal/poller"
	"github.com/styygeli/echonetgo/internal/specs"
)

const (
	operationStatusEPC = 0x80
	onStatus           = 0x30
	offStatus          = 0x31
)

// Commander subscribes to MQTT command topics and performs ECHONET SET requests.
type Commander struct {
	client     *echonet.Client
	cache      *poller.Cache
	cfg        *config.Config
	topicPrefix string
	subscribed pahomqtt.Token
}

// NewCommander creates a Commander. Call Run to subscribe and process commands.
func NewCommander(client *echonet.Client, cache *poller.Cache, cfg *config.Config, topicPrefix string) *Commander {
	return &Commander{
		client:      client,
		cache:       cache,
		cfg:         cfg,
		topicPrefix: topicPrefix,
	}
}

// Run subscribes to command topics and blocks until ctx is cancelled.
func (c *Commander) Run(ctx context.Context, mqttClient pahomqtt.Client) {
	if c.topicPrefix == "" {
		c.topicPrefix = "echonetgo"
	}
	// Subscribe to climate command topics: {prefix}/{device}/climate/#
	climateTopic := c.topicPrefix + "/+/climate/#"
	token := mqttClient.Subscribe(climateTopic, 1, c.handleClimateMessage)
	c.subscribed = token
	if !token.WaitTimeout(connectTimeout) {
		mqttLog.Warnf("commander subscribe timeout for %s", climateTopic)
		return
	}
	if err := token.Error(); err != nil {
		mqttLog.Warnf("commander subscribe failed: %v", err)
		return
	}
	// Subscribe to switch/select/number command topics
	for _, entityType := range []string{"switch", "select", "number"} {
		topic := c.topicPrefix + "/+/" + entityType + "/+/set"
		tok := mqttClient.Subscribe(topic, 1, c.handleWritableMessage)
		if !tok.WaitTimeout(connectTimeout) || tok.Error() != nil {
			mqttLog.Warnf("commander subscribe failed for %s", topic)
		}
	}
	mqttLog.Infof("commander subscribed to %s and switch/select/number", climateTopic)
	<-ctx.Done()
	_ = mqttClient.Unsubscribe(climateTopic)
	for _, entityType := range []string{"switch", "select", "number"} {
		_ = mqttClient.Unsubscribe(c.topicPrefix + "/+/" + entityType + "/+/set")
	}
}

func (c *Commander) handleClimateMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
	topic := msg.Topic()
	payload := strings.TrimSpace(string(msg.Payload()))
	if payload == "" {
		return
	}
	// topic = prefix/deviceName/climate/attr/set or prefix/deviceName/climate/attr
	parts := strings.Split(topic, "/")
	if len(parts) < 4 {
		return
	}
	// parts[0]=prefix, parts[1]=deviceName, parts[2]=climate, parts[3]=attr, optional parts[4]=set
	deviceName := parts[1]
	if parts[2] != "climate" {
		return
	}
	attr := parts[3]
	isSet := len(parts) > 4 && parts[4] == "set"
	if !isSet {
		return
	}
	dev := c.deviceByName(deviceName)
	if dev == nil {
		mqttLog.Warnf("commander: unknown device %q", deviceName)
		return
	}
	eoj, ok := c.cache.GetDeviceEOJ(*dev)
	if !ok {
		mqttLog.Warnf("commander: no EOJ for device %s", deviceName)
		return
	}
	specs, _ := c.cache.GetDeviceSpecs(*dev)
	climateSpec := c.cache.GetDeviceClimate(*dev)
	writable, _ := c.cache.GetWritableEPCs(*dev)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := dev.IP + ":3610"

	switch attr {
	case "mode":
		c.handleClimateMode(ctx, addr, eoj, dev, payload, climateSpec, specs)
	case "temperature":
		c.handleClimateTemperature(ctx, addr, eoj, dev, payload, climateSpec, specs, writable)
	case "fan_mode":
		c.handleClimateFanMode(ctx, addr, eoj, dev, payload, climateSpec, specs, writable)
	case "power":
		c.handleClimatePower(ctx, addr, eoj, dev, payload, writable)
	default:
		mqttLog.Debugf("commander: ignored climate attribute %q", attr)
	}
}

// handleWritableMessage handles switch/select/number command messages: prefix/device/switch|select|number/metricname/set
func (c *Commander) handleWritableMessage(_ pahomqtt.Client, msg pahomqtt.Message) {
	topic := msg.Topic()
	payload := strings.TrimSpace(string(msg.Payload()))
	if payload == "" {
		return
	}
	parts := strings.Split(topic, "/")
	if len(parts) != 5 || parts[4] != "set" {
		return
	}
	deviceName := parts[1]
	entityType := parts[2]
	metricName := parts[3]
	if entityType != "switch" && entityType != "select" && entityType != "number" {
		return
	}
	dev := c.deviceByName(deviceName)
	if dev == nil {
		mqttLog.Warnf("commander: unknown device %q", deviceName)
		return
	}
	eoj, ok := c.cache.GetDeviceEOJ(*dev)
	if !ok {
		mqttLog.Warnf("commander: no EOJ for device %s", deviceName)
		return
	}
	metricSpecs, ok := c.cache.GetDeviceSpecs(*dev)
	if !ok {
		return
	}
	writable, _ := c.cache.GetWritableEPCs(*dev)
	climateSpec := c.cache.GetDeviceClimate(*dev)
	ms := metricSpecByName(metricSpecs, metricName)
	if ms == nil {
		mqttLog.Warnf("commander: unknown metric %q for device %s", metricName, deviceName)
		return
	}
	if _, ok := writable[ms.EPC]; !ok {
		mqttLog.Warnf("commander: metric %s (EPC 0x%02x) not writable", metricName, ms.EPC)
		return
	}
	if isClimateEPC(ms.EPC, climateSpec) {
		return
	}
	if ms.ExcludeSet {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	addr := dev.IP + ":3610"
	c.executeWritableSet(ctx, addr, eoj, dev, ms, entityType, payload)
}

func metricSpecByName(specs []specs.MetricSpec, name string) *specs.MetricSpec {
	for i := range specs {
		if specs[i].Name == name {
			return &specs[i]
		}
	}
	return nil
}

func (c *Commander) executeWritableSet(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, ms *specs.MetricSpec, entityType, payload string) {
	var value float64
	switch entityType {
	case "switch":
		switch strings.ToUpper(payload) {
		case "ON", "1", "TRUE":
			if raw, ok := ms.ReverseEnum["on"]; ok {
				value = float64(raw)
			} else {
				value = 1
			}
		case "OFF", "0", "FALSE":
			if raw, ok := ms.ReverseEnum["off"]; ok {
				value = float64(raw)
			} else {
				value = 0
			}
		default:
			mqttLog.Warnf("commander: invalid switch payload %q", payload)
			return
		}
	case "select":
		raw, ok := ms.ReverseEnum[payload]
		if !ok {
			mqttLog.Warnf("commander: unknown select option %q for %s", payload, ms.Name)
			return
		}
		value = float64(raw)
	case "number":
		var err error
		value, err = strconv.ParseFloat(payload, 64)
		if err != nil {
			mqttLog.Warnf("commander: invalid number payload %q: %v", payload, err)
			return
		}
	default:
		return
	}
	edt, err := echonet.EncodeValueToEDT(value, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode failed for %s: %v", ms.Name, err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, ms.EPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set %s (0x%02x) failed for %s: %v", ms.Name, ms.EPC, dev.Name, err)
		return
	}
	mqttLog.Infof("commander: set %s %s = %s", dev.Name, ms.Name, payload)
}

func (c *Commander) deviceByName(name string) *config.Device {
	for i := range c.cfg.Devices {
		if c.cfg.Devices[i].Name == name {
			return &c.cfg.Devices[i]
		}
	}
	return nil
}

func (c *Commander) handleClimatePower(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, writable map[byte]struct{}) {
	if _, ok := writable[operationStatusEPC]; !ok {
		mqttLog.Warnf("commander: device %s operation_status (0x80) not writable", dev.Name)
		return
	}
	var edt []byte
	switch strings.ToUpper(payload) {
	case "ON", "1", "TRUE":
		edt = []byte{onStatus}
	case "OFF", "0", "FALSE":
		edt = []byte{offStatus}
	default:
		mqttLog.Warnf("commander: invalid power payload %q", payload)
		return
	}
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80 failed for %s: %v", dev.Name, err)
		return
	}
	mqttLog.Infof("commander: set %s power %s", dev.Name, payload)
}

func (c *Commander) handleClimateMode(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec) {
	if climateSpec == nil {
		mqttLog.Warnf("commander: device %s has no climate spec", dev.Name)
		return
	}
	payload = strings.ToLower(payload)
	if payload == "off" {
		_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{offStatus})
		if err != nil {
			mqttLog.Warnf("commander: Set 0x80=off failed for %s: %v", dev.Name, err)
			return
		}
		mqttLog.Infof("commander: set %s mode off", dev.Name)
		return
	}
	// Turn on first, then set operation mode
	_, err := c.client.SendSet(ctx, addr, eoj, operationStatusEPC, []byte{onStatus})
	if err != nil {
		mqttLog.Warnf("commander: Set 0x80=on failed for %s: %v", dev.Name, err)
		return
	}
	raw, ok := climateSpec.Modes[payload]
	if !ok || raw == nil {
		mqttLog.Warnf("commander: unknown mode %q for %s", payload, dev.Name)
		return
	}
	epc := climateSpec.ModeEPC
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for mode EPC 0x%02x", epc)
		return
	}
	edt, err := echonet.EncodeValueToEDT(float64(*raw), *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode mode failed: %v", err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set mode failed for %s: %v", dev.Name, err)
		return
	}
	mqttLog.Infof("commander: set %s mode %s", dev.Name, payload)
}

func (c *Commander) handleClimateTemperature(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
	if climateSpec == nil {
		return
	}
	epc := climateSpec.TemperatureEPC
	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s temperature EPC 0x%02x not writable", dev.Name, epc)
		return
	}
	temp, err := strconv.ParseFloat(payload, 64)
	if err != nil {
		mqttLog.Warnf("commander: invalid temperature payload %q: %v", payload, err)
		return
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil {
		mqttLog.Warnf("commander: no metric spec for temperature EPC 0x%02x", epc)
		return
	}
	edt, err := echonet.EncodeValueToEDT(temp, *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode temperature failed: %v", err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set temperature failed for %s: %v", dev.Name, err)
		return
	}
	mqttLog.Infof("commander: set %s temperature %s", dev.Name, payload)
}

func (c *Commander) handleClimateFanMode(ctx context.Context, addr string, eoj [3]byte, dev *config.Device, payload string, climateSpec *specs.ClimateSpec, metricSpecs []specs.MetricSpec, writable map[byte]struct{}) {
	if climateSpec == nil || climateSpec.FanModeEPC == 0 {
		return
	}
	epc := climateSpec.FanModeEPC
	if _, ok := writable[epc]; !ok {
		mqttLog.Warnf("commander: device %s fan_mode EPC 0x%02x not writable", dev.Name, epc)
		return
	}
	ms := metricSpecByEPC(metricSpecs, epc)
	if ms == nil || len(ms.ReverseEnum) == 0 {
		mqttLog.Warnf("commander: no metric spec or ReverseEnum for fan EPC 0x%02x", epc)
		return
	}
	raw, ok := ms.ReverseEnum[payload]
	if !ok {
		mqttLog.Warnf("commander: unknown fan_mode %q for %s", payload, dev.Name)
		return
	}
	edt, err := echonet.EncodeValueToEDT(float64(raw), *ms)
	if err != nil {
		mqttLog.Warnf("commander: encode fan_mode failed: %v", err)
		return
	}
	_, err = c.client.SendSet(ctx, addr, eoj, epc, edt)
	if err != nil {
		mqttLog.Warnf("commander: Set fan_mode failed for %s: %v", dev.Name, err)
		return
	}
	mqttLog.Infof("commander: set %s fan_mode %s", dev.Name, payload)
}

func metricSpecByEPC(specs []specs.MetricSpec, epc byte) *specs.MetricSpec {
	for i := range specs {
		if specs[i].EPC == epc {
			return &specs[i]
		}
	}
	return nil
}

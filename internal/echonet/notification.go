package echonet

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/styygeli/echonetgo/internal/logging"
	"github.com/styygeli/echonetgo/internal/model"
)

var notifLog = logging.New("notification")

// NotificationCallback is called when an INF/INFC frame is received from a known device.
type NotificationCallback func(ip string, seoj [3]byte, props []model.GetResProperty)

// NotificationHandler processes unsolicited ECHONET Lite INF/INFC frames
// received on the shared port-3610 socket.
type NotificationHandler struct {
	infChan   chan UDPFrame
	transport *Transport
	callback  NotificationCallback
	mu        sync.RWMutex
	devices   map[string]struct{} // known device IPs
}

// NewNotificationHandler creates a handler that reads from infChan,
// parses INF frames, and calls the callback for known devices.
func NewNotificationHandler(infChan chan UDPFrame, transport *Transport, cb NotificationCallback) *NotificationHandler {
	return &NotificationHandler{
		infChan:   infChan,
		transport: transport,
		callback:  cb,
		devices:   make(map[string]struct{}),
	}
}

// RegisterDevice marks an IP as a known device so its notifications are processed.
func (h *NotificationHandler) RegisterDevice(ip string) {
	h.mu.Lock()
	h.devices[ip] = struct{}{}
	h.mu.Unlock()
}

// Run processes incoming unsolicited frames until ctx is cancelled.
func (h *NotificationHandler) Run(ctx context.Context) {
	notifLog.Infof("notification handler started")
	for {
		select {
		case <-ctx.Done():
			notifLog.Infof("notification handler stopped")
			return
		case frame := <-h.infChan:
			h.handleFrame(frame)
		}
	}
}

func (h *NotificationHandler) handleFrame(frame UDPFrame) {
	if frame.From == nil {
		return
	}
	ip := frame.From.IP.String()

	inf, err := ParseINFFrame(frame.Data)
	if err != nil {
		notifLog.Debugf("invalid frame from %s: %v", ip, err)
		return
	}

	esvName := esvNameStr(inf.ESV)
	if !inf.IsNotification() {
		notifLog.Debugf("non-notification frame from %s: ESV=%s SEOJ=%s", ip, esvName, formatEOJ(inf.SEOJ))
		return
	}

	h.mu.RLock()
	_, known := h.devices[ip]
	h.mu.RUnlock()

	if !known {
		notifLog.Debugf("notification from unknown device %s: ESV=%s SEOJ=%s (%d props)",
			ip, esvName, formatEOJ(inf.SEOJ), len(inf.Props))
		return
	}

	notifLog.Infof("INF from %s: ESV=%s SEOJ=%s props=%d", ip, esvName, formatEOJ(inf.SEOJ), len(inf.Props))
	for _, p := range inf.Props {
		notifLog.Debugf("  EPC=0x%02x PDC=%d EDT=%x", p.EPC, p.PDC, p.EDT)
	}

	if inf.ESV == esvINFC {
		h.sendINFCRes(frame.From, inf)
	}

	if h.callback != nil && len(inf.Props) > 0 {
		h.callback(ip, inf.SEOJ, inf.Props)
	}
}

func (h *NotificationHandler) sendINFCRes(addr *net.UDPAddr, inf *INFFrame) {
	conn := h.transport.FixedConn()
	if conn == nil {
		notifLog.Warnf("cannot send INFC_Res to %s: no shared socket", addr.IP)
		return
	}
	resp := BuildINFCRes(inf)
	if _, err := conn.WriteToUDP(resp, addr); err != nil {
		notifLog.Warnf("failed to send INFC_Res to %s: %v", addr.IP, err)
		return
	}
	notifLog.Debugf("sent INFC_Res to %s for SEOJ=%s", addr.IP, formatEOJ(inf.SEOJ))
}

func esvNameStr(esv byte) string {
	switch esv {
	case esvGet:
		return "Get"
	case esvGetRes:
		return "Get_Res"
	case esvSetC:
		return "SetC"
	case esvINF:
		return "INF"
	case esvINFC:
		return "INFC"
	case esvINFCRes:
		return "INFC_Res"
	case esvINFSNA:
		return "INF_SNA"
	default:
		return fmt.Sprintf("0x%02x", esv)
	}
}

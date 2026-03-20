package echonet

import (
	"testing"

	"github.com/styygeli/echonetgo/internal/model"
)

func TestParseINFFrame_ValidINF(t *testing.T) {
	// INF frame: SEOJ=0x026b01 (ecocute), DEOJ=0x0ef001, ESV=0x73 (INF), 1 property
	frame := []byte{
		0x10, 0x81,       // EHD
		0x00, 0x42,       // TID
		0x02, 0x6B, 0x01, // SEOJ (ecocute)
		0x0E, 0xF0, 0x01, // DEOJ (node profile)
		0x73,             // ESV = INF
		0x01,             // OPC = 1
		0xC3, 0x01, 0x41, // EPC=0xC3, PDC=1, EDT=0x41
	}
	inf, err := ParseINFFrame(frame)
	if err != nil {
		t.Fatalf("ParseINFFrame() error = %v", err)
	}
	if inf.TID != 0x0042 {
		t.Fatalf("TID = 0x%04x, want 0x0042", inf.TID)
	}
	if inf.SEOJ != [3]byte{0x02, 0x6B, 0x01} {
		t.Fatalf("SEOJ = %v, want [0x02, 0x6B, 0x01]", inf.SEOJ)
	}
	if inf.DEOJ != [3]byte{0x0E, 0xF0, 0x01} {
		t.Fatalf("DEOJ = %v, want [0x0E, 0xF0, 0x01]", inf.DEOJ)
	}
	if inf.ESV != esvINF {
		t.Fatalf("ESV = 0x%02x, want 0x73", inf.ESV)
	}
	if !inf.IsNotification() {
		t.Fatal("IsNotification() should return true for ESV=INF")
	}
	if len(inf.Props) != 1 {
		t.Fatalf("len(Props) = %d, want 1", len(inf.Props))
	}
	if inf.Props[0].EPC != 0xC3 {
		t.Fatalf("Props[0].EPC = 0x%02x, want 0xC3", inf.Props[0].EPC)
	}
	if len(inf.Props[0].EDT) != 1 || inf.Props[0].EDT[0] != 0x41 {
		t.Fatalf("Props[0].EDT = %v, want [0x41]", inf.Props[0].EDT)
	}
}

func TestParseINFFrame_INFC(t *testing.T) {
	frame := []byte{
		0x10, 0x81,
		0x00, 0x01,
		0x01, 0x30, 0x01,
		0x05, 0xFF, 0x01,
		0x74,             // ESV = INFC
		0x01,
		0x80, 0x01, 0x30,
	}
	inf, err := ParseINFFrame(frame)
	if err != nil {
		t.Fatalf("ParseINFFrame() error = %v", err)
	}
	if !inf.IsNotification() {
		t.Fatal("IsNotification() should return true for ESV=INFC")
	}
}

func TestIsNotification_GetRes(t *testing.T) {
	frame := []byte{
		0x10, 0x81,
		0x00, 0x01,
		0x01, 0x30, 0x01,
		0x05, 0xFF, 0x01,
		0x72,             // ESV = Get_Res (not a notification)
		0x01,
		0x80, 0x01, 0x30,
	}
	inf, err := ParseINFFrame(frame)
	if err != nil {
		t.Fatalf("ParseINFFrame() error = %v", err)
	}
	if inf.IsNotification() {
		t.Fatal("IsNotification() should return false for ESV=Get_Res")
	}
}

func TestParseINFFrame_TooShort(t *testing.T) {
	_, err := ParseINFFrame([]byte{0x10, 0x81, 0x00})
	if err == nil {
		t.Fatal("ParseINFFrame() expected error for short frame")
	}
}

func TestParseINFFrame_MultipleProperties(t *testing.T) {
	frame := []byte{
		0x10, 0x81,
		0x00, 0x10,
		0x01, 0x30, 0x01,
		0x0E, 0xF0, 0x01,
		0x73,             // INF
		0x02,             // 2 properties
		0x80, 0x01, 0x30, // operation_status = ON
		0xB0, 0x01, 0x42, // operation_mode = cool
	}
	inf, err := ParseINFFrame(frame)
	if err != nil {
		t.Fatalf("ParseINFFrame() error = %v", err)
	}
	if len(inf.Props) != 2 {
		t.Fatalf("len(Props) = %d, want 2", len(inf.Props))
	}
	if inf.Props[0].EPC != 0x80 || inf.Props[1].EPC != 0xB0 {
		t.Fatalf("Props EPCs = [0x%02x, 0x%02x], want [0x80, 0xB0]", inf.Props[0].EPC, inf.Props[1].EPC)
	}
}

func TestBuildINFCRes(t *testing.T) {
	inf := &INFFrame{
		TID:  0x0042,
		SEOJ: [3]byte{0x01, 0x30, 0x01},
		ESV:  esvINFC,
		Props: []model.GetResProperty{
			{EPC: 0x80, PDC: 1, EDT: []byte{0x30}},
		},
	}
	resp := BuildINFCRes(inf)
	if len(resp) < 12 {
		t.Fatalf("response too short: %d bytes", len(resp))
	}
	// ESV should be INFC_Res (0x7A)
	if resp[10] != esvINFCRes {
		t.Fatalf("ESV = 0x%02x, want 0x7A", resp[10])
	}
	// TID preserved
	if resp[2] != 0x00 || resp[3] != 0x42 {
		t.Fatalf("TID = 0x%02x%02x, want 0x0042", resp[2], resp[3])
	}
}

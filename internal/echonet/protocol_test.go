package echonet

import (
	"bytes"
	"testing"
)

func TestGetRequest(t *testing.T) {
	eoj := [3]byte{0x02, 0x88, 0x01}
	frame := GetRequest(0x1234, eoj, []byte{0x80, 0xE0})

	want := []byte{
		0x10, 0x81, // EHD
		0x12, 0x34, // TID
		0x05, 0xFF, 0x01, // SEOJ (controller)
		0x02, 0x88, 0x01, // DEOJ
		0x62,       // ESV Get
		0x02,       // OPC
		0x80, 0x00, // EPC 0x80, PDC 0
		0xE0, 0x00, // EPC 0xE0, PDC 0
	}
	if !bytes.Equal(frame, want) {
		t.Fatalf("GetRequest() = % x, want % x", frame, want)
	}
}

func TestSetRequest(t *testing.T) {
	eoj := [3]byte{0x01, 0x30, 0x01}
	frame := SetRequest(0x0001, eoj, 0xB3, []byte{0x19})

	want := []byte{
		0x10, 0x81,
		0x00, 0x01,
		0x05, 0xFF, 0x01,
		0x01, 0x30, 0x01,
		0x61,       // ESV SetC
		0x01,       // OPC
		0xB3, 0x01, // EPC, PDC
		0x19, // EDT
	}
	if !bytes.Equal(frame, want) {
		t.Fatalf("SetRequest() = % x, want % x", frame, want)
	}
}

func TestParseFrame_TruncatedPropertyIsSkipped(t *testing.T) {
	// OPC claims 1 property with PDC=4 but only 2 EDT bytes are present.
	frame := []byte{
		0x10, 0x81,
		0x00, 0x01,
		0x01, 0x30, 0x01,
		0x05, 0xFF, 0x01,
		0x72,       // Get_Res
		0x01,       // OPC=1
		0xE0, 0x04, // EPC 0xE0, PDC=4
		0x00, 0x10, // only 2 bytes, not 4
	}
	_, _, props, err := parseFrame(frame)
	if err != nil {
		t.Fatalf("parseFrame() error = %v", err)
	}
	if len(props) != 0 {
		t.Fatalf("expected truncated property to be dropped, got %d props", len(props))
	}
}

func TestParseFrame_ShortFrame(t *testing.T) {
	if _, _, _, err := parseFrame([]byte{0x10, 0x81, 0x00}); err == nil {
		t.Fatal("parseFrame() expected error for short frame")
	}
}

func TestParseFrame_BadEHD(t *testing.T) {
	frame := make([]byte, minResponseLen)
	frame[0] = 0x00 // not 0x10
	frame[1] = 0x81
	if _, _, _, err := parseFrame(frame); err == nil {
		t.Fatal("parseFrame() expected error for invalid EHD")
	}
}

func TestParseINFFrameAndINFCRes(t *testing.T) {
	// INFC (0x74) frame from an AC reporting operation_status.
	frame := []byte{
		0x10, 0x81,
		0xAB, 0xCD, // TID
		0x01, 0x30, 0x01, // SEOJ (device)
		0x05, 0xFF, 0x01, // DEOJ (controller)
		0x74,       // ESV INFC
		0x01,       // OPC
		0x80, 0x01, // EPC 0x80, PDC 1
		0x30, // EDT (on)
	}
	inf, err := ParseINFFrame(frame)
	if err != nil {
		t.Fatalf("ParseINFFrame() error = %v", err)
	}
	if !inf.IsNotification() {
		t.Fatal("IsNotification() = false, want true for INFC")
	}
	if inf.SEOJ != [3]byte{0x01, 0x30, 0x01} {
		t.Fatalf("SEOJ = % x, want 01 30 01", inf.SEOJ)
	}
	if inf.TID != 0xABCD {
		t.Fatalf("TID = 0x%04x, want 0xABCD", inf.TID)
	}

	// The ack must echo TID + the device's SEOJ as its DEOJ, with ESV 0x7A.
	res := BuildINFCRes(inf)
	wantHead := []byte{
		0x10, 0x81,
		0xAB, 0xCD,
		0x05, 0xFF, 0x01, // controller as source
		0x01, 0x30, 0x01, // device as destination
		0x7A, // INFC_Res
		0x01, // OPC
		0x80, 0x01, 0x30,
	}
	if !bytes.Equal(res, wantHead) {
		t.Fatalf("BuildINFCRes() = % x, want % x", res, wantHead)
	}
}

func TestDecodePropertyMap_Bitmap0xE0(t *testing.T) {
	// Bitmap format (>= 17 bytes). Set the bit that decodes to EPC 0xE0.
	// code = 0xE0 & 0x0F = 0x00 -> byte index 1; row = (0xE0>>4)-8 = 14-8 = 6 -> bit 6.
	edt := make([]byte, 17)
	edt[0] = 0x01   // count
	edt[1] = 1 << 6 // bit 6 in column 0 -> EPC 0xE0
	got := decodePropertyMap(edt)
	if _, ok := got[0xE0]; !ok {
		t.Fatalf("expected EPC 0xE0 in decoded bitmap, got %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 EPC, got %d", len(got))
	}
}

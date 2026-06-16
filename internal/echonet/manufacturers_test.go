package echonet

import "testing"

func TestFormatManufacturerCode(t *testing.T) {
	tests := []struct {
		name string
		edt  []byte
		want string
	}{
		{"valid 3-byte code", []byte{0x00, 0x00, 0x06}, "000006"},
		{"valid full-range code", []byte{0xAB, 0xCD, 0xEF}, "abcdef"},
		{"empty edt", nil, ""},
		{"short edt (1 byte)", []byte{0x00}, ""},
		{"short edt (2 bytes)", []byte{0x00, 0x00}, ""},
		{"long edt (4 bytes)", []byte{0x00, 0x00, 0x06, 0x00}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Must not panic on any input length: a device returning 0x8A with a
			// short PDC previously crashed the bridge via an unguarded index.
			got := formatManufacturerCode(tt.edt)
			if got != tt.want {
				t.Fatalf("formatManufacturerCode(%x) = %q, want %q", tt.edt, got, tt.want)
			}
		})
	}
}

func TestDecodeManufacturer(t *testing.T) {
	t.Run("known code", func(t *testing.T) {
		if got := decodeManufacturer([]byte{0x00, 0x00, 0x06}); got != "Mitsubishi Electric" {
			t.Fatalf("decodeManufacturer = %q, want Mitsubishi Electric", got)
		}
	})
	t.Run("unknown code falls back to hex", func(t *testing.T) {
		if got := decodeManufacturer([]byte{0xFF, 0xFF, 0xFF}); got != "0xFFFFFF" {
			t.Fatalf("decodeManufacturer = %q, want 0xFFFFFF", got)
		}
	})
	t.Run("short edt does not panic", func(t *testing.T) {
		if got := decodeManufacturer([]byte{0x00}); got != "" {
			t.Fatalf("decodeManufacturer(short) = %q, want empty", got)
		}
	})
}

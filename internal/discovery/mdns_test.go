package discovery

import (
	"testing"
	"time"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		name      string
		addr      string
		wantHost  string
		wantPort  int
		wantError bool
	}{
		{
			name:     "valid address",
			addr:     "192.168.1.100:7777",
			wantHost: "192.168.1.100",
			wantPort: 7777,
		},
		{
			name:     "localhost",
			addr:     "localhost:8080",
			wantHost: "localhost",
			wantPort: 8080,
		},
		{
			name:      "missing port",
			addr:      "192.168.1.100",
			wantError: true,
		},
		{
			name:      "invalid port",
			addr:      "192.168.1.100:abc",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port, err := ParseAddress(tt.addr)

			if tt.wantError {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if host != tt.wantHost {
				t.Errorf("host = %s, want %s", host, tt.wantHost)
			}

			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
		})
	}
}

func TestGetLocalIP(t *testing.T) {
	ip, err := GetLocalIP()
	if err != nil {
		t.Logf("GetLocalIP returned error (expected on some CI environments): %v", err)
		return
	}

	if ip == "" {
		t.Error("IP should not be empty")
	}

	t.Logf("Local IP: %s", ip)
}

func TestNewHostResolver(t *testing.T) {
	roomKey := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	resolver := NewHostResolver(roomKey, 5*time.Second)

	if resolver.timeout != 5*time.Second {
		t.Errorf("timeout = %v, want 5s", resolver.timeout)
	}

	if resolver.service == "" {
		t.Error("service name should not be empty")
	}
}

func TestNewHostAdvertiser(t *testing.T) {
	roomKey := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF}
	advertiser := NewHostAdvertiser(roomKey, 7777)

	if advertiser.port != 7777 {
		t.Errorf("port = %d, want 7777", advertiser.port)
	}

	if advertiser.service == "" {
		t.Error("service name should not be empty")
	}
}

package discovery

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/hashicorp/mdns"
	"github.com/zuhailz/GoDrop/internal/crypto"
)

const (
	ServiceName   = "_godrop._tcp"
	ServiceDomain = "local."
)

type HostAdvertiser struct {
	server  *mdns.Server
	roomKey []byte // raw room key bytes
	service string // derived mDNS instance name
	port    int
}

func NewHostAdvertiser(roomKey []byte, port int) *HostAdvertiser {
	return &HostAdvertiser{
		roomKey: roomKey,
		service: crypto.DeriveServiceName(roomKey),
		port:    port,
	}
}

func (h *HostAdvertiser) Start() error {
	var ips []net.IP
	if localIP, err := GetLocalIP(); err == nil {
		ips = []net.IP{net.ParseIP(localIP)}
	}

	service, err := mdns.NewMDNSService(
		h.service,
		ServiceName,
		ServiceDomain,
		"",
		h.port,
		ips,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create mDNS service: %w", err)
	}

	server, err := mdns.NewServer(&mdns.Config{Zone: service})
	if err != nil {
		return fmt.Errorf("failed to start mDNS server: %w", err)
	}

	h.server = server
	return nil
}

func (h *HostAdvertiser) Stop() {
	if h.server != nil {
		_ = h.server.Shutdown()
	}
}

type HostResolver struct {
	service string // derived mDNS instance name
	timeout time.Duration
}

func NewHostResolver(roomKey []byte, timeout time.Duration) *HostResolver {
	return &HostResolver{
		service: crypto.DeriveServiceName(roomKey),
		timeout: timeout,
	}
}

func (r *HostResolver) Resolve() (string, int, error) {
	ip, port, err := r.query(false, true)
	if err == nil {
		return ip, port, nil
	}
	return r.query(true, false)
}

func (r *HostResolver) query(disableV4, disableV6 bool) (string, int, error) {
	entriesCh := make(chan *mdns.ServiceEntry, 10)
	target := r.service + "." + ServiceName + "." + ServiceDomain

	params := mdns.DefaultParams(ServiceName)
	params.Domain = ServiceDomain
	params.Entries = entriesCh
	params.Timeout = r.timeout
	params.DisableIPv4 = disableV4
	params.DisableIPv6 = disableV6

	errCh := make(chan error, 1)
	go func() {
		errCh <- mdns.Query(params)
	}()

	for {
		select {
		case entry := <-entriesCh:
			if entry.Name == target {
				addr := entry.AddrV4
				if addr == nil && entry.AddrV6IPAddr != nil {
					addr = entry.AddrV6IPAddr.IP
				}
				return addr.String(), entry.Port, nil
			}

		case err := <-errCh:
			if err != nil {
				return "", 0, fmt.Errorf("mDNS query failed: %w", err)
			}
			return "", 0, fmt.Errorf("host not found (wrong room key, or no host on this network)")
		}
	}
}

func GetLocalIP() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", err
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String(), nil
			}
		}
	}

	return "", fmt.Errorf("no local IP address found")
}

func ParseAddress(addr string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid address format: %w", err)
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %w", err)
	}

	return host, port, nil
}

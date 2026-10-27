package main

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/huin/goupnp/dcps/internetgateway2"
)

type NetInfo struct {
	LanIP    string `json:"lanIP"`
	PublicIP string `json:"publicIP"`
	RouterIP string `json:"routerIP"` // external IP the router reports via UPnP
	Port     int    `json:"port"`
	UPnP     string `json:"upnp"` // "", "trying", "ok", "failed", "off"
	CGNAT    bool   `json:"cgnat"`
}

// lanIP returns the address other devices in the home network use to reach
// this PC. No packet is actually sent.
func lanIP() string {
	c, err := net.Dial("udp4", "1.1.1.1:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).IP.String()
}

func publicIP() string {
	for _, u := range []string{"https://api.ipify.org", "https://ifconfig.me/ip", "https://icanhazip.com"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req.Header.Set("User-Agent", userAgent)
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			resp.Body.Close()
			cancel()
			ip := strings.TrimSpace(string(b))
			if net.ParseIP(ip) != nil {
				return ip
			}
			continue
		}
		cancel()
	}
	return ""
}

type igdClient interface {
	AddPortMapping(remoteHost string, extPort uint16, proto string, intPort uint16, intClient string, enabled bool, desc string, lease uint32) error
	DeletePortMapping(remoteHost string, extPort uint16, proto string) error
	GetExternalIPAddress() (string, error)
}

type portMapping struct {
	client igdClient
	port   uint16
}

func (p *portMapping) Remove() {
	if p != nil {
		p.client.DeletePortMapping("", p.port, "TCP")
	}
}

// addPortMapping asks the router (via UPnP) to forward the Minecraft port
// to this PC. Many routers have UPnP on by default.
func addPortMapping(port int, localIP string) (*portMapping, string, error) {
	var clients []igdClient
	if cs, _, err := internetgateway2.NewWANIPConnection2Clients(); err == nil {
		for _, c := range cs {
			clients = append(clients, c)
		}
	}
	if cs, _, err := internetgateway2.NewWANIPConnection1Clients(); err == nil {
		for _, c := range cs {
			clients = append(clients, c)
		}
	}
	if cs, _, err := internetgateway2.NewWANPPPConnection1Clients(); err == nil {
		for _, c := range cs {
			clients = append(clients, c)
		}
	}
	if len(clients) == 0 {
		return nil, "", errors.New("no UPnP router found; UPnP may be switched off in the router settings")
	}
	var lastErr error
	for _, c := range clients {
		// Lease 0 = until removed. Some routers reject 0, so retry with 24h.
		err := c.AddPortMapping("", uint16(port), "TCP", uint16(port), localIP, true, "MC EasyHost", 0)
		if err != nil {
			err = c.AddPortMapping("", uint16(port), "TCP", uint16(port), localIP, true, "MC EasyHost", 86400)
		}
		if err != nil {
			lastErr = err
			continue
		}
		ext, _ := c.GetExternalIPAddress()
		return &portMapping{client: c, port: uint16(port)}, ext, nil
	}
	return nil, "", lastErr
}

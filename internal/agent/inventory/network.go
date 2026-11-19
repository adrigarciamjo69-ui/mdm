package inventory

import (
	"net"

	"mdm/internal/protocol"
)

// collectNetwork lista las interfaces de red activas, sin la de bucle local.
func collectNetwork() []protocol.NetIf {
	out := []protocol.NetIf{}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		n := protocol.NetIf{Name: ifc.Name, MAC: ifc.HardwareAddr.String(), IPv4: []string{}, IPv6: []string{}}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.IsLinkLocalUnicast() {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				n.IPv4 = append(n.IPv4, ip4.String())
			} else {
				n.IPv6 = append(n.IPv6, ipnet.IP.String())
			}
		}
		if n.MAC == "" && len(n.IPv4) == 0 && len(n.IPv6) == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

package helpers

import (
	"fmt"
	"net"
)

// GetARecord returns the first A record (IPv4 address) for the provided host.
// It returns an error if no A record is found.
func GetARecord(host string) (net.IP, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			return ip4, nil
		}
	}
	return nil, fmt.Errorf("no A record found for host: %s", host)
}

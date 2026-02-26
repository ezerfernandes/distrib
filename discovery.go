package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

const (
	discoveryMagic    = "DISTRIB-DISCOVER"
	discoveryResponse = "DISTRIB-HERE"
)

type Peer struct {
	Name string
	Addr string // "ip:httpPort"
}

func discoverPeers(discoveryPort int, timeout time.Duration) ([]Peer, error) {
	conn, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("open UDP socket: %w", err)
	}
	defer conn.Close()

	msg := []byte(discoveryMagic + "\n")

	// Send to 255.255.255.255
	broadcastAddr := &net.UDPAddr{IP: net.IPv4(255, 255, 255, 255), Port: discoveryPort}
	if _, err := conn.WriteTo(msg, broadcastAddr); err != nil {
		log.Printf("broadcast to 255.255.255.255: %v", err)
	}

	// Also send to each interface's directed broadcast address
	for _, ip := range interfaceBroadcastAddrs() {
		addr := &net.UDPAddr{IP: ip, Port: discoveryPort}
		if _, err := conn.WriteTo(msg, addr); err != nil {
			log.Printf("broadcast to %s: %v", ip, err)
		}
	}

	conn.SetReadDeadline(time.Now().Add(timeout))

	seen := make(map[string]bool)
	var peers []Peer
	buf := make([]byte, 1024)

	for {
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			break // timeout or error
		}

		line := strings.TrimSpace(string(buf[:n]))
		if !strings.HasPrefix(line, discoveryResponse) {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) != 3 {
			continue
		}

		name := parts[1]
		httpPort := parts[2]

		host, _, _ := net.SplitHostPort(addr.String())
		peerAddr := net.JoinHostPort(host, httpPort)

		if seen[peerAddr] {
			continue
		}
		seen[peerAddr] = true

		peers = append(peers, Peer{Name: name, Addr: peerAddr})
	}

	return peers, nil
}

func listenForDiscovery(ctx context.Context, discoveryPort int, name string, httpPort int) error {
	addr := &net.UDPAddr{Port: discoveryPort}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listen UDP :%d: %w", discoveryPort, err)
	}
	defer conn.Close()

	log.Printf("Discovery listener on :%d", discoveryPort)

	response := []byte(fmt.Sprintf("%s %s %d\n", discoveryResponse, name, httpPort))
	buf := make([]byte, 1024)

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("UDP read error: %v", err)
			continue
		}

		line := strings.TrimSpace(string(buf[:n]))
		if line == discoveryMagic {
			log.Printf("Discovery request from %s", remoteAddr)
			if _, err := conn.WriteToUDP(response, remoteAddr); err != nil {
				log.Printf("UDP reply error: %v", err)
			}
		}
	}
}

func interfaceBroadcastAddrs() []net.IP {
	var addrs []net.IP

	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}

		ifAddrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, a := range ifAddrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}

			ip4 := ipNet.IP.To4()
			if ip4 == nil {
				continue
			}

			// Compute broadcast: ip | ~mask
			mask := ipNet.Mask
			broadcast := make(net.IP, 4)
			for i := range ip4 {
				broadcast[i] = ip4[i] | ^mask[i]
			}

			addrs = append(addrs, broadcast)
		}
	}

	return addrs
}

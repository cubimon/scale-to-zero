package main

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

func main() {
	dnsServer := "127.0.0.1:53"
	resolver := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: time.Millisecond * time.Duration(2000),
			}
			fmt.Println("dns lookup address", address)
			return d.DialContext(ctx, "udp", dnsServer)
		},
	}
	for i := range 100 {
		_, err := resolver.LookupHost(context.Background(), "service"+strconv.Itoa(i))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
	}
}

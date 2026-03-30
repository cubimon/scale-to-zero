package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"
)

func main() {
	client := &http.Client{}
	dnsServer := "127.0.0.1:10053"
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
		ips, err := resolver.LookupHost(context.Background(), "service"+strconv.Itoa(i))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println(ips)
		resp, err := client.Get("http://" + ips[0])
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("Status: %s\n", resp.Status)
	}
}

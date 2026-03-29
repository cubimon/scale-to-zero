package main

import (
	"flag"
	"fmt"
	"net/http"
)

func main() {
	targetUrl := flag.String("target", "http://172.20.0.3", "The URL to request")
	flag.Parse()

	client := &http.Client{}

	resp, err := client.Get(*targetUrl)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}
	fmt.Printf("Status: %s\n", resp.Status)
}

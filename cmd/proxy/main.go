package main

import (
	"fmt"

	"zeroscale.cubimon.github.io/registry"
)

func main() {
	registry, err := registry.NewRegistry()
	if err != nil {
		fmt.Println("Failed to create service registry")
		return
	}
	registry.Start()
}

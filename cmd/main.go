package main

import (
	"fmt"
	"os"

	"HyperBot/boot"
)

func main() {
	if err := boot.Boot(); err != nil {
		fmt.Fprintln(os.Stderr, "hyperbot:", err)
		os.Exit(1)
	}
}

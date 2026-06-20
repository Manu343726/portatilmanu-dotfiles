package main

import (
	"os"
)

var buildHash string

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

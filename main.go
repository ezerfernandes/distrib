package main

import (
	"fmt"
	"os"
)

const (
	defaultHTTPPort      = 9848
	defaultDiscoveryPort = 9847
	version              = "0.1.0"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "push":
		cmdPush(os.Args[2:])
	case "version":
		fmt.Println("distrib", version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `distrib - distribute HTML files across your local network

Usage:
  distrib serve [flags]        Start the receiver daemon
  distrib push <file> [flags]  Push an HTML file to peers
  distrib version              Print version

Run 'distrib <command> -help' for details.
`)
}

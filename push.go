package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func cmdPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	target := fs.String("target", "", "Target address (host:port), skips discovery")
	discoveryPort := fs.Int("discovery-port", defaultDiscoveryPort, "UDP discovery port")
	timeout := fs.Duration("timeout", 2*time.Second, "Discovery timeout")
	fs.Parse(args)

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: distrib push <file.html> [flags]")
		os.Exit(1)
	}

	filePath := fs.Arg(0)

	data, err := os.ReadFile(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", filePath, err)
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	var peers []Peer

	if *target != "" {
		peers = []Peer{{Name: *target, Addr: *target}}
	} else {
		fmt.Println("Discovering peers...")
		peers, err = discoverPeers(*discoveryPort, *timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: discovery failed: %v\n", err)
			os.Exit(1)
		}

		if len(peers) == 0 {
			fmt.Fprintln(os.Stderr, "No peers found.")
			fmt.Fprintln(os.Stderr, "If you're in WSL2, try: distrib push <file> -target <ip:port>")
			os.Exit(1)
		}

		fmt.Printf("Found %d peer(s):\n", len(peers))
		for i, p := range peers {
			fmt.Printf("  %d. %s (%s)\n", i+1, p.Name, p.Addr)
		}
	}

	filename := filepath.Base(filePath)

	for _, peer := range peers {
		fmt.Printf("Pushing %s to %s... ", filename, peer.Name)

		id, err := pushFile(peer.Addr, filename, hostname, data)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}

		fmt.Printf("OK (id: %s)\n", id)
	}
}

func pushFile(addr, filename, sender string, data []byte) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}

	if err := writer.WriteField("sender", sender); err != nil {
		return "", fmt.Errorf("write sender field: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart: %w", err)
	}

	url := fmt.Sprintf("http://%s/receive", addr)
	resp, err := http.Post(url, writer.FormDataContentType(), &body)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("server returned %d: %s", resp.StatusCode, respBody)
	}

	var result struct {
		OK bool   `json:"ok"`
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return result.ID, nil
}

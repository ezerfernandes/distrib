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

func cmdPushAssets(args []string) {
	fs := flag.NewFlagSet("push-assets", flag.ExitOnError)
	htmlFile := fs.String("for", "", "HTML filename this asset belongs to (required)")
	target := fs.String("target", "", "Target address (host:port), skips discovery")
	discoveryPort := fs.Int("discovery-port", defaultDiscoveryPort, "UDP discovery port")
	timeout := fs.Duration("timeout", 2*time.Second, "Discovery timeout")
	fs.Parse(args)

	if *htmlFile == "" {
		fmt.Fprintln(os.Stderr, "Error: --for flag is required (HTML filename)")
		fmt.Fprintln(os.Stderr, "Usage: distrib push-assets --for <file.html> <asset1> [asset2] ... [flags]")
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Usage: distrib push-assets --for <file.html> <asset1> [asset2] ... [flags]")
		os.Exit(1)
	}

	// Read all asset files
	var assets []assetData
	for i := 0; i < fs.NArg(); i++ {
		path := fs.Arg(i)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: cannot read %s: %v\n", path, err)
			os.Exit(1)
		}
		assets = append(assets, assetData{name: filepath.Base(path), data: data})
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
		var err error
		peers, err = discoverPeers(*discoveryPort, *timeout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: discovery failed: %v\n", err)
			os.Exit(1)
		}

		if len(peers) == 0 {
			fmt.Fprintln(os.Stderr, "No peers found.")
			os.Exit(1)
		}

		fmt.Printf("Found %d peer(s):\n", len(peers))
		for i, p := range peers {
			fmt.Printf("  %d. %s (%s)\n", i+1, p.Name, p.Addr)
		}
	}

	for _, peer := range peers {
		fmt.Printf("Pushing %d asset(s) for %s to %s... ", len(assets), *htmlFile, peer.Name)

		id, err := pushAssets(peer.Addr, *htmlFile, hostname, assets)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}

		fmt.Printf("OK (id: %s)\n", id)
	}
}

type assetData struct {
	name string
	data []byte
}

func pushAssets(addr, htmlFilename, sender string, assets []assetData) (string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("sender", sender); err != nil {
		return "", fmt.Errorf("write sender field: %w", err)
	}

	if err := writer.WriteField("for", htmlFilename); err != nil {
		return "", fmt.Errorf("write for field: %w", err)
	}

	for _, asset := range assets {
		part, err := writer.CreateFormFile("files", asset.name)
		if err != nil {
			return "", fmt.Errorf("create form file %s: %w", asset.name, err)
		}
		if _, err := part.Write(asset.data); err != nil {
			return "", fmt.Errorf("write file data %s: %w", asset.name, err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart: %w", err)
	}

	url := fmt.Sprintf("http://%s/receive-assets", addr)
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

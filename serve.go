package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

//go:embed web/index.html
var webFS embed.FS

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	port := fs.Int("port", defaultHTTPPort, "HTTP port")
	discoveryPort := fs.Int("discovery-port", defaultDiscoveryPort, "UDP discovery port")
	name := fs.String("name", "", "Machine name (default: hostname)")
	dataDir := fs.String("data", "", "Data directory (default: ~/.distrib)")
	fs.Parse(args)

	if *name == "" {
		hostname, err := os.Hostname()
		if err != nil {
			hostname = "unknown"
		}
		*name = hostname
	}

	if *dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("Cannot determine home directory: %v", err)
		}
		*dataDir = filepath.Join(home, ".distrib")
	}

	store, err := NewStore(*dataDir)
	if err != nil {
		log.Fatalf("Initialize storage: %v", err)
	}

	broker := NewSSEBroker()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		if err := listenForDiscovery(ctx, *discoveryPort, *name, *port); err != nil {
			log.Printf("Discovery listener error: %v", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /receive", handleReceive(store, broker))
	mux.HandleFunc("POST /receive-assets", handleReceiveAssets(store, broker))
	mux.HandleFunc("GET /files", handleFiles(store))
	mux.HandleFunc("DELETE /files/{id}", handleFileDelete(store, broker))
	mux.HandleFunc("GET /files/{id}", handleFileView(store))
	mux.HandleFunc("GET /files/{id}/raw", handleFileRawRedirect())
	mux.HandleFunc("GET /files/{id}/raw/", handleFileRaw(store))
	mux.HandleFunc("GET /files/{id}/raw/{path...}", handleFileAsset(store))
	mux.HandleFunc("GET /events", broker.ServeHTTP)
	mux.HandleFunc("GET /health", handleHealth(*name))
	mux.HandleFunc("GET /", handleIndex())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		server.Shutdown(shutdownCtx)
	}()

	log.Printf("Distrib serving on :%d as %q", *port, *name)
	log.Printf("Web UI: http://localhost:%d/files", *port)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("HTTP server: %v", err)
	}
}

func handleReceive(store *Store, broker *SSEBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			jsonError(w, "parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		file, header, err := r.FormFile("file")
		if err != nil {
			jsonError(w, "missing file field", http.StatusBadRequest)
			return
		}
		defer file.Close()

		sender := r.FormValue("sender")
		if sender == "" {
			sender = "unknown"
		}

		data, err := io.ReadAll(file)
		if err != nil {
			jsonError(w, "read file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		entry, updated, err := store.Save(header.Filename, sender, data)
		if err != nil {
			jsonError(w, "save file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if updated {
			log.Printf("Updated %q from %s (%d bytes)", entry.Filename, entry.Sender, entry.Size)
			go sendNotification("Distrib", fmt.Sprintf("Updated %s from %s", entry.Filename, entry.Sender))
			broker.PublishUpdate(entry)
		} else {
			log.Printf("Received %q from %s (%d bytes)", entry.Filename, entry.Sender, entry.Size)
			go sendNotification("Distrib", fmt.Sprintf("Received %s from %s", entry.Filename, entry.Sender))
			broker.Publish(entry)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": entry.ID})
	}
}

func handleFiles(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		if !strings.Contains(accept, "application/json") {
			handleIndex()(w, r)
			return
		}

		files, err := store.List()
		if err != nil {
			jsonError(w, "list files: "+err.Error(), http.StatusInternalServerError)
			return
		}

		if files == nil {
			files = []FileEntry{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(files)
	}
}

func handleFileDelete(store *Store, broker *SSEBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := store.Delete(id); err != nil {
			jsonError(w, "delete file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("Deleted file %s", id)
		broker.PublishRemoval(id)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func handleFileView(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		entry, err := store.Get(id)
		if err != nil {
			jsonError(w, "file not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entry)
	}
}

func handleFileRawRedirect() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
	}
}

func handleFileRaw(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		path, err := store.FilePath(id)
		if err != nil {
			http.Error(w, "invalid ID", http.StatusBadRequest)
			return
		}
		http.ServeFile(w, r, path)
	}
}

func handleFileAsset(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		assetPath := r.PathValue("path")

		dir, err := store.ContentDirPath(id)
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Sanitize: only allow base filename, no path traversal
		safeName := filepath.Base(assetPath)
		fullPath := filepath.Join(dir, safeName)
		http.ServeFile(w, r, fullPath)
	}
}

func handleReceiveAssets(store *Store, broker *SSEBroker) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(50 << 20); err != nil {
			jsonError(w, "parse form: "+err.Error(), http.StatusBadRequest)
			return
		}

		sender := r.FormValue("sender")
		if sender == "" {
			sender = "unknown"
		}

		htmlFilename := r.FormValue("for")
		if htmlFilename == "" {
			jsonError(w, "missing 'for' field (HTML filename)", http.StatusBadRequest)
			return
		}

		entry := store.FindByFilenameAndSender(htmlFilename, sender)
		if entry == nil {
			jsonError(w, fmt.Sprintf("no file %q from sender %q found", htmlFilename, sender), http.StatusNotFound)
			return
		}

		// Collect all uploaded asset files
		var assetNames []string
		fhs := r.MultipartForm.File["files"]
		for _, fh := range fhs {
			f, err := fh.Open()
			if err != nil {
				jsonError(w, "open uploaded file: "+err.Error(), http.StatusInternalServerError)
				return
			}
			data, err := io.ReadAll(f)
			f.Close()
			if err != nil {
				jsonError(w, "read uploaded file: "+err.Error(), http.StatusInternalServerError)
				return
			}

			if err := store.SaveAsset(entry.ID, fh.Filename, data); err != nil {
				jsonError(w, "save asset: "+err.Error(), http.StatusInternalServerError)
				return
			}

			assetNames = append(assetNames, filepath.Base(fh.Filename))
			log.Printf("Saved asset %q for %q from %s", fh.Filename, htmlFilename, sender)
		}

		// Rewrite URLs in the HTML file
		if len(assetNames) > 0 {
			if err := rewriteHTMLUrls(store, entry, assetNames); err != nil {
				log.Printf("Warning: failed to rewrite HTML URLs: %v", err)
			}
		}

		broker.PublishUpdate(entry)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "id": entry.ID})
	}
}

func rewriteHTMLUrls(store *Store, entry *FileEntry, assetNames []string) error {
	htmlPath, err := store.FilePath(entry.ID)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(htmlPath)
	if err != nil {
		return err
	}

	content := string(data)
	for _, name := range assetNames {
		// Match src="...name" or href="...name" and replace the path with just the filename
		pattern := `((?:src|href)\s*=\s*["'])([^"']*` + regexp.QuoteMeta(name) + `)(["'])`
		re := regexp.MustCompile(pattern)
		content = re.ReplaceAllString(content, "${1}"+name+"${3}")
	}

	return os.WriteFile(htmlPath, []byte(content), 0644)
}

func handleHealth(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"name": name, "status": "ok"})
	}
}

func handleIndex() http.HandlerFunc {
	htmlData, err := webFS.ReadFile("web/index.html")
	if err != nil {
		log.Fatalf("embedded web UI not found: %v", err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(htmlData)
	}
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

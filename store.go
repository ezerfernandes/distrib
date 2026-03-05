package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var validID = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[a-f0-9]{6}$`)

type FileEntry struct {
	ID         string    `json:"id"`
	Filename   string    `json:"filename"`
	Sender     string    `json:"sender"`
	ReceivedAt time.Time `json:"received_at"`
	Size       int64     `json:"size"`
	SHA256     string    `json:"sha256"`
	ContentDir string    `json:"content_dir"`
}

type Store struct {
	baseDir string
}

func NewStore(dataDir string) (*Store, error) {
	filesDir := filepath.Join(dataDir, "files")
	if err := os.MkdirAll(filesDir, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &Store{baseDir: filesDir}, nil
}

// Save stores a file. If a file with the same filename and sender already exists,
// it updates that entry in place. Returns the entry and whether it was an update.
func (s *Store) Save(filename, sender string, data []byte) (*FileEntry, bool, error) {
	hash := sha256.Sum256(data)
	hashHex := hex.EncodeToString(hash[:])
	now := time.Now()

	// Check for existing file with same filename+sender
	if existing := s.FindByFilenameAndSender(filename, sender); existing != nil {
		return s.update(existing.ID, filename, sender, data, hashHex, now)
	}

	id := fmt.Sprintf("%s-%s", now.Format("20060102-150405"), hashHex[:6])

	contentDir := filenameWithoutExt(filename)
	entryDir := filepath.Join(s.baseDir, id)
	contentPath := filepath.Join(entryDir, contentDir)
	if err := os.MkdirAll(contentPath, 0755); err != nil {
		return nil, false, fmt.Errorf("create content dir: %w", err)
	}

	filePath := filepath.Join(contentPath, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return nil, false, fmt.Errorf("write file: %w", err)
	}

	entry := &FileEntry{
		ID:         id,
		Filename:   filename,
		Sender:     sender,
		ReceivedAt: now,
		Size:       int64(len(data)),
		SHA256:     hashHex,
		ContentDir: contentDir,
	}

	metaPath := filepath.Join(entryDir, "meta.json")
	metaData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, false, fmt.Errorf("write metadata: %w", err)
	}

	return entry, false, nil
}

func (s *Store) update(id, filename, sender string, data []byte, hashHex string, now time.Time) (*FileEntry, bool, error) {
	contentDir := filenameWithoutExt(filename)
	entryDir := filepath.Join(s.baseDir, id)
	contentPath := filepath.Join(entryDir, contentDir)
	if err := os.MkdirAll(contentPath, 0755); err != nil {
		return nil, false, fmt.Errorf("create content dir: %w", err)
	}

	filePath := filepath.Join(contentPath, filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return nil, false, fmt.Errorf("write file: %w", err)
	}

	entry := &FileEntry{
		ID:         id,
		Filename:   filename,
		Sender:     sender,
		ReceivedAt: now,
		Size:       int64(len(data)),
		SHA256:     hashHex,
		ContentDir: contentDir,
	}

	metaPath := filepath.Join(entryDir, "meta.json")
	metaData, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, false, fmt.Errorf("marshal metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, metaData, 0644); err != nil {
		return nil, false, fmt.Errorf("write metadata: %w", err)
	}

	return entry, true, nil
}

func (s *Store) FindByFilenameAndSender(filename, sender string) *FileEntry {
	entries, err := s.List()
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Filename == filename && e.Sender == sender {
			return &e
		}
	}
	return nil
}

func (s *Store) List() ([]FileEntry, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("read storage dir: %w", err)
	}

	var files []FileEntry
	for _, e := range entries {
		if !e.IsDir() || !validID.MatchString(e.Name()) {
			continue
		}
		entry, err := s.Get(e.Name())
		if err != nil {
			continue
		}
		files = append(files, *entry)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].ReceivedAt.After(files[j].ReceivedAt)
	})

	return files, nil
}

func (s *Store) Get(id string) (*FileEntry, error) {
	if !validID.MatchString(id) {
		return nil, fmt.Errorf("invalid file ID")
	}

	metaPath := filepath.Join(s.baseDir, id, "meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	var entry FileEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	return &entry, nil
}

func (s *Store) Delete(id string) error {
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid file ID")
	}
	entryDir := filepath.Join(s.baseDir, id)
	if err := os.RemoveAll(entryDir); err != nil {
		return fmt.Errorf("delete entry: %w", err)
	}
	return nil
}

func (s *Store) FilePath(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid file ID")
	}

	// Try new structure first: {id}/{contentDir}/{filename}
	entry, err := s.Get(id)
	if err == nil && entry.ContentDir != "" {
		return filepath.Join(s.baseDir, id, entry.ContentDir, entry.Filename), nil
	}

	// Backward compat: old entries stored as original.html
	return filepath.Join(s.baseDir, id, "original.html"), nil
}

func (s *Store) ContentDirPath(id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("invalid file ID")
	}

	entry, err := s.Get(id)
	if err != nil {
		return "", err
	}
	if entry.ContentDir == "" {
		return "", fmt.Errorf("entry has no content directory")
	}

	return filepath.Join(s.baseDir, id, entry.ContentDir), nil
}

func (s *Store) SaveAsset(id string, assetName string, data []byte) error {
	dir, err := s.ContentDirPath(id)
	if err != nil {
		return err
	}

	// Sanitize: only use the base name to prevent path traversal
	safeName := filepath.Base(assetName)
	assetPath := filepath.Join(dir, safeName)
	return os.WriteFile(assetPath, data, 0644)
}

func filenameWithoutExt(filename string) string {
	ext := filepath.Ext(filename)
	return filename[:len(filename)-len(ext)]
}

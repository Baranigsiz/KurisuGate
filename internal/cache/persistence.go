package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// SnapshotData is the top-level persisted JSON structure
type SnapshotData struct {
	Version   string                  `json:"version"`
	Timestamp time.Time               `json:"timestamp"`
	Exact     []ExactCacheEntryExport `json:"exact_entries"`
	Semantic  []SemanticEntryExport   `json:"semantic_entries"`
}

// SnapshotManager coordinates saving and loading cache snapshots to/from disk
type SnapshotManager struct {
	filePath string
	mu       sync.Mutex
}

// NewSnapshotManager creates a cache snapshot manager
func NewSnapshotManager(filePath string) *SnapshotManager {
	if filePath == "" {
		filePath = "./data/kurisu_cache.json"
	}
	return &SnapshotManager{
		filePath: filePath,
	}
}

// SaveSnapshot atomically serializes cache data to disk
func (m *SnapshotManager) SaveSnapshot(exact *ExactCache, semantic *SemanticCache) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data := SnapshotData{
		Version:   "1.0",
		Timestamp: time.Now(),
	}

	if exact != nil {
		data.Exact = exact.ExportEntries()
	}
	if semantic != nil {
		data.Semantic = semantic.ExportEntries()
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache snapshot: %w", err)
	}

	dir := filepath.Dir(m.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Atomic write via temporary file
	tmpFile := fmt.Sprintf("%s.tmp.%d", m.filePath, time.Now().UnixNano())
	if err := os.WriteFile(tmpFile, raw, 0644); err != nil {
		return fmt.Errorf("failed to write temporary snapshot file: %w", err)
	}

	if err := os.Rename(tmpFile, m.filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("failed to commit cache snapshot: %w", err)
	}

	return nil
}

// RestoreSnapshot reads the snapshot from disk and populates the caches
func (m *SnapshotManager) RestoreSnapshot(exact *ExactCache, semantic *SemanticCache) (int, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := os.Stat(m.filePath); os.IsNotExist(err) {
		return 0, 0, nil // No existing snapshot, fresh start
	}

	raw, err := os.ReadFile(m.filePath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read cache snapshot file: %w", err)
	}

	var data SnapshotData
	if err := json.Unmarshal(raw, &data); err != nil {
		return 0, 0, fmt.Errorf("failed to parse cache snapshot: %w", err)
	}

	var exactImported, semanticImported int
	if exact != nil && len(data.Exact) > 0 {
		exactImported = exact.ImportEntries(data.Exact)
	}
	if semantic != nil && len(data.Semantic) > 0 {
		semanticImported = semantic.ImportEntries(data.Semantic)
	}

	return exactImported, semanticImported, nil
}

// StartAutoSnapshot begins background periodic snapshots
func (m *SnapshotManager) StartAutoSnapshot(ctx context.Context, exact *ExactCache, semantic *SemanticCache, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = m.SaveSnapshot(exact, semantic)
			}
		}
	}()
}

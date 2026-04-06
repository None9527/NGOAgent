package knowledge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// SearchResult holds a single vector search result.
type SearchResult struct {
	ID    string
	Score float64 // cosine similarity
}

// VectorIndex is an in-memory brute-force vector index with disk persistence.
// Designed for <1000 items — no need for HNSW/FAISS at this scale.
type VectorIndex struct {
	mu         sync.RWMutex
	vectors    map[string][]float32 // id → embedding
	dimensions int
	indexDir   string // persistence directory
}

// NewVectorIndex creates a new index. Call Load() to populate from disk.
func NewVectorIndex(dimensions int, indexDir string) *VectorIndex {
	os.MkdirAll(indexDir, 0755)
	return &VectorIndex{
		vectors:    make(map[string][]float32),
		dimensions: dimensions,
		indexDir:   indexDir,
	}
}

// Add inserts or replaces a vector for the given ID.
func (idx *VectorIndex) Add(id string, vec []float32) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.vectors[id] = vec
}

// Remove deletes a vector by ID.
func (idx *VectorIndex) Remove(id string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.vectors, id)
}

// Search returns the top-K most similar vectors by cosine similarity.
func (idx *VectorIndex) Search(query []float32, topK int) []SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	var results []SearchResult
	for id, vec := range idx.vectors {
		score := cosineSimilarity(query, vec)
		results = append(results, SearchResult{ID: id, Score: score})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if topK > 0 && len(results) > topK {
		results = results[:topK]
	}
	return results
}

// Size returns the number of indexed vectors.
func (idx *VectorIndex) Size() int {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.vectors)
}

// Has checks if an ID exists in the index.
func (idx *VectorIndex) Has(id string) bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	_, ok := idx.vectors[id]
	return ok
}

// GetVec returns the raw vector for an ID, or nil if not found.
func (idx *VectorIndex) GetVec(id string) []float32 {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.vectors[id]
}

// ═══════════════════════════════════════════
// Disk persistence
// ═══════════════════════════════════════════

type indexMapping struct {
	Dimensions int      `json:"dimensions"`
	IDs        []string `json:"ids"` // ordered, maps to vectors.bin offsets
}

// Save writes the index to disk: mapping.json + vectors.bin
func (idx *VectorIndex) Save() error {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	if len(idx.vectors) == 0 {
		return nil
	}

	// Build ordered ID list
	ids := make([]string, 0, len(idx.vectors))
	for id := range idx.vectors {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	// Write mapping
	mapping := indexMapping{Dimensions: idx.dimensions, IDs: ids}
	mData, _ := json.MarshalIndent(mapping, "", "  ")
	if err := os.WriteFile(filepath.Join(idx.indexDir, "mapping.json"), mData, 0644); err != nil {
		return fmt.Errorf("write mapping: %w", err)
	}

	// Write vectors as raw float32 binary
	f, err := os.Create(filepath.Join(idx.indexDir, "vectors.bin"))
	if err != nil {
		return fmt.Errorf("create vectors.bin: %w", err)
	}
	defer f.Close()

	for _, id := range ids {
		vec := idx.vectors[id]
		for _, v := range vec {
			if err := binary.Write(f, binary.LittleEndian, v); err != nil {
				return fmt.Errorf("write vector: %w", err)
			}
		}
	}

	return nil
}

// Load reads the index from disk.
func (idx *VectorIndex) Load() error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	mPath := filepath.Join(idx.indexDir, "mapping.json")
	mData, err := os.ReadFile(mPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no index yet
		}
		return fmt.Errorf("read mapping: %w", err)
	}

	var mapping indexMapping
	if err := json.Unmarshal(mData, &mapping); err != nil {
		return fmt.Errorf("parse mapping: %w", err)
	}

	vPath := filepath.Join(idx.indexDir, "vectors.bin")
	vData, err := os.ReadFile(vPath)
	if err != nil {
		return fmt.Errorf("read vectors: %w", err)
	}

	dims := mapping.Dimensions
	if dims == 0 {
		dims = idx.dimensions
	}

	expectedBytes := len(mapping.IDs) * dims * 4
	if len(vData) < expectedBytes {
		return fmt.Errorf("vectors.bin too small: %d < %d", len(vData), expectedBytes)
	}

	idx.vectors = make(map[string][]float32, len(mapping.IDs))
	for i, id := range mapping.IDs {
		vec := make([]float32, dims)
		offset := i * dims * 4
		for j := 0; j < dims; j++ {
			bits := binary.LittleEndian.Uint32(vData[offset+j*4 : offset+j*4+4])
			vec[j] = math.Float32frombits(bits)
		}
		idx.vectors[id] = vec
	}
	idx.dimensions = dims

	return nil
}

// ═══════════════════════════════════════════
// Math
// ═══════════════════════════════════════════

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

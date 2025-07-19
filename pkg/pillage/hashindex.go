package pillage

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"sync"
)

// HashIndex keeps track of image hashes that have already been scanned.
type HashIndex struct {
	path string
	mu   sync.Mutex
	set  map[string]struct{}
}

// NewHashIndex loads or creates a hash index at the given path.
func NewHashIndex(path string) (*HashIndex, error) {
	hi := &HashIndex{path: path, set: make(map[string]struct{})}
	// Ensure file exists
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDONLY, 0666)
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		hi.set[scanner.Text()] = struct{}{}
	}
	f.Close()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return hi, nil
}

// Exists returns true if the hash is already recorded.
func (h *HashIndex) Exists(hash string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.set[hash]
	return ok
}

// Add records the given hash if it isn't already stored.
func (h *HashIndex) Add(hash string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.set[hash]; ok {
		return nil
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(hash + "\n"); err != nil {
		f.Close()
		return err
	}
	f.Close()
	h.set[hash] = struct{}{}
	return nil
}

// AddIfMissing checks if the hash exists and records it atomically.
// It returns true if the hash was already present.
func (h *HashIndex) AddIfMissing(hash string) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.set[hash]; ok {
		return true, nil
	}
	f, err := os.OpenFile(h.path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return false, err
	}
	if _, err := f.WriteString(hash + "\n"); err != nil {
		f.Close()
		return false, err
	}
	f.Close()
	h.set[hash] = struct{}{}
	return false, nil
}

// ImageHash returns the SHA256 of the image's manifest.
func ImageHash(img *ImageData) string {
	sum := sha256.Sum256([]byte(img.Manifest))
	return hex.EncodeToString(sum[:])
}

package adapters

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// Registry manages PEFT adapter metadata on disk under dir/registry.json.
type Registry struct {
	mu  sync.RWMutex
	dir string
}

type registryFile struct {
	Adapters []domain.AdapterMeta `json:"adapters"`
}

// New opens (or creates) an adapter registry rooted at dir.
func New(dir string) (*Registry, error) {
	if dir == "" {
		dir = "peft-adapters"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("adapters dir: %w", err)
	}
	r := &Registry{dir: dir}
	if _, err := os.Stat(r.path()); os.IsNotExist(err) {
		if err := r.write(registryFile{Adapters: nil}); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *Registry) path() string {
	return filepath.Join(r.dir, "registry.json")
}

func (r *Registry) read() (registryFile, error) {
	raw, err := os.ReadFile(r.path())
	if err != nil {
		if os.IsNotExist(err) {
			return registryFile{}, nil
		}
		return registryFile{}, err
	}
	var f registryFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return registryFile{}, err
	}
	return f, nil
}

func (r *Registry) write(f registryFile) error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path() + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path())
}

// Dir returns the adapters root directory.
func (r *Registry) Dir() string { return r.dir }

// List returns adapters with Active flagged for activeID.
func (r *Registry) List(activeID string) ([]domain.AdapterMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.read()
	if err != nil {
		return nil, err
	}
	out := make([]domain.AdapterMeta, 0, len(f.Adapters))
	for _, a := range f.Adapters {
		a.Active = a.ID == activeID
		out = append(out, a)
	}
	return out, nil
}

// Upsert registers or updates an adapter and optionally writes a marker file.
func (r *Registry) Upsert(meta domain.AdapterMeta) (domain.AdapterMeta, error) {
	meta.ID = strings.TrimSpace(meta.ID)
	if meta.ID == "" {
		return domain.AdapterMeta{}, fmt.Errorf("%w: adapter id required", domain.ErrValidation)
	}
	if meta.Name == "" {
		meta.Name = meta.ID
	}
	if meta.Path == "" {
		meta.Path = meta.ID
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.read()
	if err != nil {
		return domain.AdapterMeta{}, err
	}
	found := false
	for i, a := range f.Adapters {
		if a.ID == meta.ID {
			f.Adapters[i] = meta
			found = true
			break
		}
	}
	if !found {
		f.Adapters = append(f.Adapters, meta)
	}
	marker := filepath.Join(r.dir, meta.Path)
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(r.dir, meta.Path+".adapter"), []byte(meta.ID+"\n"), 0o644)
	}
	if err := r.write(f); err != nil {
		return domain.AdapterMeta{}, err
	}
	return meta, nil
}

// Remove deletes an adapter from the registry.
func (r *Registry) Remove(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return domain.ErrValidation
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	f, err := r.read()
	if err != nil {
		return err
	}
	next := f.Adapters[:0]
	found := false
	for _, a := range f.Adapters {
		if a.ID == id {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		return domain.ErrNotFound
	}
	f.Adapters = next
	return r.write(f)
}

// Get returns one adapter by id.
func (r *Registry) Get(id string) (domain.AdapterMeta, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, err := r.read()
	if err != nil {
		return domain.AdapterMeta{}, err
	}
	for _, a := range f.Adapters {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.AdapterMeta{}, domain.ErrNotFound
}

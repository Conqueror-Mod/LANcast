package milkdrop

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
 * The presets on disk: `<data>/visualizers/milkdrop/<id>.json`, one file per
 * preset, named by the SHA-256 of the preset itself.
 *
 * WHY NO TABLE
 *
 * Everything a list shows — name, format, what happened to its shaders, when
 * it came — is written into the preset's own file, so the file is the whole
 * truth and there is nothing to reconcile. A table would be a second copy of
 * that truth, and the two could disagree: a row whose file had gone, a file no
 * row named. Saves (internal/retro/saves) and plugins keep their bytes on disk
 * for the same reason the ADR gives — a backup of the database should not grow
 * with every upload.
 *
 * The name of a file is never anything a person sent. It is a hash, checked
 * against a pattern before any path is built from it, and the path is then
 * checked to be inside the folder anyway: the containment rule, applied to an
 * id that comes from a URL rather than a row.
 *
 * The list is read from disk once and kept in memory, then kept current by
 * Put and Delete. Nothing else writes the folder: one server runs per machine.
 *
 * A .milk's HLSL is in the same file, under "hlsl", beside the preset rather
 * than in it: Get never returns it, so it is never served. It is kept so that
 * a better converter can convert a stored preset again without anybody
 * finding the file to upload it a second time. Presets stored before step g3
 * have none — that version dropped the HLSL — so for those a re-upload is the
 * only way to their shaders, and Put makes a re-upload replace them.
 */

// ErrNotFound means there is no preset with that id, or it is not an id.
var ErrNotFound = errors.New("no such preset")

var idPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Summary is what a list shows about one preset.
type Summary struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Source  string `json:"source"`
	Shaders string `json:"shaders"`
	// ShadersReason is why a .milk's shaders were not converted, when that is
	// known: the converter's first error line, named by shader. A preset
	// stored before the server could convert shaders has none.
	ShadersReason string `json:"shaders_reason,omitempty"`
	SizeBytes     int64  `json:"size_bytes"`
	UploadedAt    int64  `json:"uploaded_at"`
}

// Entry is a stored preset: its summary and the preset itself. It is what is
// served.
type Entry struct {
	Summary
	Preset Preset `json:"preset"`
}

// stored is the file: an Entry and, for a .milk with shaders, their HLSL.
type stored struct {
	Entry
	HLSL *ShaderSource `json:"hlsl,omitempty"`
}

// indexed is what the in-memory list keeps about one file.
type indexed struct {
	Summary
	hasHLSL bool
}

// Store is the folder of presets.
type Store struct {
	root string

	mu    sync.Mutex
	index map[string]indexed // nil until first read
}

// NewStore keeps presets under root, creating it when the first one comes.
func NewStore(root string) *Store { return &Store{root: root} }

// Root is the folder, for messages.
func (s *Store) Root() string { return s.root }

// path is the file for id, or ErrNotFound for anything that is not one.
func (s *Store) path(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrNotFound
	}
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	p, err := filepath.Abs(filepath.Join(root, id+".json"))
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(root, p); err != nil || rel != id+".json" {
		return "", fmt.Errorf("preset path %q escapes %q", p, root)
	}
	return p, nil
}

// load reads the folder into the index the first time it is needed. A file
// that does not read as a preset is left out of the list rather than failing
// it: one damaged file must not hide every other preset.
func (s *Store) load() error {
	if s.index != nil {
		return nil
	}
	idx := map[string]indexed{}
	des, err := os.ReadDir(s.root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read presets: %w", err)
	}
	for _, de := range des {
		id, ok := strings.CutSuffix(de.Name(), ".json")
		if !ok || !idPattern.MatchString(id) || !de.Type().IsRegular() {
			continue
		}
		e, err := s.read(id)
		if err != nil {
			continue
		}
		idx[id] = indexed{Summary: e.Summary, hasHLSL: e.HLSL != nil}
	}
	s.index = idx
	return nil
}

func (s *Store) read(id string) (stored, error) {
	p, err := s.path(id)
	if err != nil {
		return stored{}, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return stored{}, ErrNotFound
	}
	if err != nil {
		return stored{}, fmt.Errorf("read preset: %w", err)
	}
	var e stored
	if err := json.Unmarshal(b, &e); err != nil {
		return stored{}, fmt.Errorf("read preset %s: %w", id, err)
	}
	if e.ID != id {
		return stored{}, fmt.Errorf("preset file %s names itself %s", id, e.ID)
	}
	return e, nil
}

// List is every preset, by name.
func (s *Store) List() ([]Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(s.index))
	for _, ix := range s.index {
		out = append(out, ix.Summary)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if a != b {
			return a < b
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Get is one preset, as it is served: without its HLSL.
func (s *Store) Get(id string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.read(id)
	return e.Entry, err
}

// HLSL is the shader text a stored .milk came with, or nil. It is for the
// server alone; no handler serves it.
func (s *Store) HLSL(id string) (*ShaderSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.read(id)
	return e.HLSL, err
}

/*
 * Put stores an upload. created is false when the same preset was already
 * there, in which case the stored one is returned: its name is the one it was
 * first given, because uploading a pack twice should not rename anything.
 *
 * Two cases are about the same .milk stored earlier without its shaders —
 * before step g3, which dropped them and kept no HLSL, or by a converter that
 * could not convert them then:
 *
 *   - When this upload's shaders converted and u.Replaces names such a
 *     preset, the new preset takes its place, under its name, and the old
 *     file is removed. replaced is its id. Otherwise re-uploading a folder to
 *     get its shaders would put a second copy of every preset beside the
 *     first.
 *   - When the shaders still do not convert, the preset is the same one, so
 *     its id is too. A file from before g3 gains the HLSL and the reason,
 *     keeping its name and time, and created is false.
 *
 * The file is written beside its final name and renamed into place, so a
 * reader never sees half a preset.
 */
func (s *Store) Put(u Upload) (sm Summary, created bool, replaced string, err error) {
	id, err := ID(u.Preset)
	if err != nil {
		return Summary{}, false, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Summary{}, false, "", err
	}
	if have, ok := s.index[id]; ok {
		if have.hasHLSL || u.HLSL == nil {
			return have.Summary, false, "", nil
		}
		e, err := s.read(id)
		if err != nil {
			return Summary{}, false, "", err
		}
		e.Shaders, e.ShadersReason, e.HLSL = u.Shaders, u.ShadersReason, u.HLSL
		if err := s.write(id, e); err != nil {
			return Summary{}, false, "", err
		}
		return e.Summary, false, "", nil
	}
	name := u.Name
	old, superseding := s.index[u.Replaces]
	superseding = superseding && u.Replaces != id &&
		old.Source == SourceMilk && old.Shaders == ShadersNotConverted
	if superseding {
		name = old.Name
	}
	preset, err := json.Marshal(u.Preset)
	if err != nil {
		return Summary{}, false, "", err
	}
	e := stored{Entry: Entry{Summary: Summary{ID: id, Name: name, Source: u.Source, Shaders: u.Shaders,
		ShadersReason: u.ShadersReason, SizeBytes: int64(len(preset)), UploadedAt: time.Now().Unix()},
		Preset: u.Preset}, HLSL: u.HLSL}
	if err := s.write(id, e); err != nil {
		return Summary{}, false, "", err
	}
	if superseding {
		// The new file is in place first, so a failure here leaves two
		// presets rather than none.
		if p, err := s.path(u.Replaces); err == nil {
			if err := os.Remove(p); err == nil || errors.Is(err, os.ErrNotExist) {
				delete(s.index, u.Replaces)
				replaced = u.Replaces
			}
		}
	}
	return e.Summary, true, replaced, nil
}

// write puts e in its file and in the index. The caller holds s.mu.
func (s *Store) write(id string, e stored) error {
	p, err := s.path(id)
	if err != nil {
		return err
	}
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.root, 0o755); err != nil {
		return fmt.Errorf("create preset folder: %w", err)
	}
	tmp, err := os.CreateTemp(s.root, ".upload-*")
	if err != nil {
		return fmt.Errorf("store preset: %w", err)
	}
	_, werr := tmp.Write(body)
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), p)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("store preset: %w", werr)
	}
	s.index[id] = indexed{Summary: e.Summary, hasHLSL: e.HLSL != nil}
	return nil
}

// Delete removes a preset. It returns the summary it removed, for the audit
// log.
func (s *Store) Delete(id string) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return Summary{}, err
	}
	p, err := s.path(id)
	if err != nil {
		return Summary{}, err
	}
	ix, ok := s.index[id]
	if err := os.Remove(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			delete(s.index, id)
			return Summary{}, ErrNotFound
		}
		return Summary{}, fmt.Errorf("delete preset: %w", err)
	}
	delete(s.index, id)
	sm := ix.Summary
	if !ok {
		sm = Summary{ID: id}
	}
	return sm, nil
}

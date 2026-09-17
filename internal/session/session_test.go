package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSaveProtectsTokensOnDisk pins the properties that keep a captured
// OAuth2 access or refresh token out of a commit and out of other users'
// reach: 0700 on .apic/, 0600 on session.json, and a .gitignore beside it.
func TestSaveProtectsTokensOnDisk(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("dev", map[string]string{"token": "secret-token"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	gi := filepath.Join(root, Dir, ".gitignore")
	data, err := os.ReadFile(gi)
	if err != nil {
		t.Fatalf("session directory needs a .gitignore: %v", err)
	}
	if string(data) == "" {
		t.Fatal(".gitignore should not be empty")
	}

	// File modes are not meaningful on Windows.
	if runtime.GOOS == "windows" {
		return
	}
	dirInfo, err := os.Stat(filepath.Join(root, Dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("%s mode = %o, want 700", Dir, perm)
	}
	fileInfo, err := os.Stat(filepath.Join(root, Dir, File))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("%s mode = %o, want 600", File, perm)
	}
	giInfo, err := os.Stat(gi)
	if err != nil {
		t.Fatal(err)
	}
	if perm := giInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf(".gitignore mode = %o, want 600", perm)
	}
}

func TestSaveOpenRoundTrip(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("dev", map[string]string{"token": "t1", "id": "7"})
	s.Set("prod", map[string]string{"token": "t2"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}

	again, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := again.Get("dev", "token"); !ok || v != "t1" {
		t.Errorf("dev token = %q, %v", v, ok)
	}
	if v, ok := again.Get("prod", "token"); !ok || v != "t2" {
		t.Errorf("prod token = %q, %v", v, ok)
	}
	if _, ok := again.Get("dev", "nope"); ok {
		t.Error("unknown name should not resolve")
	}
	if got := again.EnvNames(); len(got) != 2 || got[0] != "dev" || got[1] != "prod" {
		t.Errorf("EnvNames = %v, want [dev prod] sorted", got)
	}
}

// TestOpenMissingAndCorrupt covers the two states a fresh or damaged project
// can be in: no session file at all is fine, unreadable JSON is not.
func TestOpenMissingAndCorrupt(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatalf("a missing session file should be fine: %v", err)
	}
	if len(s.EnvNames()) != 0 {
		t.Errorf("a fresh session should be empty, got %v", s.EnvNames())
	}

	if err := os.MkdirAll(filepath.Join(root, Dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Dir, File), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root); err == nil {
		t.Error("a corrupt session file should be an error, not silent data loss")
	}
}

// TestDefaultEnvKey pins that "" and the default name address the same bucket,
// so a run without --env finds what an earlier one captured.
func TestDefaultEnvKey(t *testing.T) {
	s := NewMemory()
	s.Set("", map[string]string{"token": "t"})
	if v, ok := s.Get(DefaultEnv, "token"); !ok || v != "t" {
		t.Errorf("empty env should be stored under %q, got %q %v", DefaultEnv, v, ok)
	}
	if v, ok := s.Get("", "token"); !ok || v != "t" {
		t.Errorf("empty env should read back, got %q %v", v, ok)
	}
}

func TestVarsIsACopy(t *testing.T) {
	s := NewMemory()
	s.Set("dev", map[string]string{"token": "t"})
	vars := s.Vars("dev")
	vars["token"] = "mutated"
	vars["extra"] = "added"
	if v, _ := s.Get("dev", "token"); v != "t" {
		t.Errorf("Vars should hand out a copy, store now has %q", v)
	}
	if _, ok := s.Get("dev", "extra"); ok {
		t.Error("Vars should hand out a copy, store gained a key")
	}
}

func TestClear(t *testing.T) {
	s := NewMemory()
	s.Set("dev", map[string]string{"token": "t"})
	s.Set("prod", map[string]string{"token": "t"})

	s.Clear("dev")
	if _, ok := s.Get("dev", "token"); ok {
		t.Error("dev should be cleared")
	}
	if _, ok := s.Get("prod", "token"); !ok {
		t.Error("prod should survive clearing dev")
	}

	s.Clear("*")
	if got := s.EnvNames(); len(got) != 0 {
		t.Errorf(`Clear("*") should drop everything, got %v`, got)
	}
}

// TestMemoryStoreWritesNothing pins --no-session: nothing reaches the disk.
func TestMemoryStoreWritesNothing(t *testing.T) {
	root := t.TempDir()
	s := NewMemory()
	s.Set("dev", map[string]string{"token": "secret-token"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("a memory store should write nothing, found %v", entries)
	}
}

// TestSavedFileShape guards the on-disk format, which a user may inspect and
// which Open must keep reading.
func TestSavedFileShape(t *testing.T) {
	root := t.TempDir()
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("dev", map[string]string{"token": "t"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, Dir, File))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Envs map[string]map[string]string `json:"envs"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Envs["dev"]["token"] != "t" {
		t.Errorf("unexpected shape: %s", data)
	}
}

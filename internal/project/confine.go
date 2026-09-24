package project

import (
	"errors"
	"io/fs"
	"path/filepath"
)

// ErrOutsideRoot is a path that resolves outside the project root.
var ErrOutsideRoot = errors.New("resolves outside project root")

// Confine resolves rel against dir (an absolute rel as it is), follows
// symlinks, and returns the real path if it lies under root, else
// ErrOutsideRoot. The runner reads and writes files through it, and
// validate checks them with it, so the two agree on what is inside.
func Confine(root, dir, rel string) (string, error) {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	joined := rel
	if !filepath.IsAbs(rel) {
		joined = filepath.Join(dir, rel)
	}
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		real = RealPrefix(abs)
	}
	if _, ok := Within(rootReal, real); !ok {
		return "", ErrOutsideRoot
	}
	return real, nil
}

// RealPrefix resolves the symlinks of the longest existing ancestor of a
// path that does not exist yet (a `>> file` into a new directory) and
// keeps the rest as written, so a root under a symlink (macOS's /var is
// /private/var) still contains what it should.
func RealPrefix(abs string) string {
	rest := ""
	for cur := abs; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

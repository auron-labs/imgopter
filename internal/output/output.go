// Package output plans collision-free batches and safely replaces output files.
package output

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"imgopter"
	"io"
	"os"
	"path/filepath"
)

// Job holds a source and its validated, resolved destination.
type Job struct {
	Source string
	Dest   string
}

// Plan resolves every destination before writing anything. Overwrite permits
// replacing a job's own source only; other inputs and outputs remain protected.
// destination must only compute a name, without changing the filesystem.
func Plan(inputs []string, format imgopter.Format, destination func(string, imgopter.Format) string, overwrite bool) ([]Job, error) {
	jobs := make([]Job, len(inputs))
	sources := make([]string, len(inputs))
	infos := make([]os.FileInfo, len(inputs))
	for i, src := range inputs {
		var err error
		sources[i], err = resolve(src)
		if err != nil {
			return nil, err
		}
		infos[i], err = os.Stat(sources[i])
		if err != nil {
			return nil, err
		}
		if !infos[i].Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file", src)
		}
		f := format
		if f == imgopter.FormatAuto {
			f, err = inputFormat(src)
			if err != nil {
				return nil, err
			}
		}
		dest, err := resolve(destination(src, f))
		if err != nil {
			return nil, err
		}
		jobs[i] = Job{Source: src, Dest: dest}
	}
	for i, job := range jobs {
		destInfo, err := os.Stat(job.Dest)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for j, src := range sources {
			same := job.Dest == src || (destInfo != nil && os.SameFile(destInfo, infos[j]))
			if same && (!overwrite || i != j) {
				return nil, fmt.Errorf("output %s would overwrite input %s", job.Dest, inputs[j])
			}
		}
		for j := range i {
			otherInfo, err := os.Stat(jobs[j].Dest)
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			if job.Dest == jobs[j].Dest || (destInfo != nil && otherInfo != nil && os.SameFile(destInfo, otherInfo)) {
				return nil, fmt.Errorf("inputs %s and %s share output %s", inputs[j], job.Source, job.Dest)
			}
		}
	}
	return jobs, nil
}

func inputFormat(path string) (imgopter.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return imgopter.FormatAuto, err
	}
	defer func() { _ = f.Close() }()
	var header [512]byte
	n, err := io.ReadFull(f, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return imgopter.FormatAuto, err
	}
	format := imgopter.SniffFormat(header[:n])
	if format == imgopter.FormatAuto {
		format = imgopter.FormatPNG
	}
	return format, nil
}

// resolve follows existing symlinks, including parent directories of a new path.
func resolve(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// A dangling symlink is not a safe new destination.
	if _, statErr := os.Lstat(path); statErr == nil {
		return "", err
	}
	parent := filepath.Dir(path)
	if parent == path {
		return "", err
	}
	parent, err = resolve(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

// WriteFile writes and closes a temporary file before replacing path. Existing
// permissions are preserved. A failed write, sync, close or rename leaves the
// destination intact. Rename replaces a final symlink rather than following it.
func WriteFile(path string, data []byte) error {
	return writeAtomic(path, func(w io.Writer) error {
		_, err := io.Copy(w, bytes.NewReader(data))
		return err
	})
}

func writeAtomic(path string, write func(io.Writer) error) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	info, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if info != nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		mode = info.Mode().Perm()
	}
	// O_EXCL and a random name create a staging file without following links.
	// Creation honors the umask for new outputs.
	temp := filepath.Join(dir, ".imgopter-"+rand.Text())
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp) }()
	defer func() { _ = f.Close() }()
	if err := write(f); err != nil {
		return err
	}
	if info != nil {
		if err := f.Chmod(mode); err != nil {
			return err
		}
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

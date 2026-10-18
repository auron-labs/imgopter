package output

import (
	"bytes"
	"errors"
	"imgopter"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func put(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanProtectsInputsAndDestinations(t *testing.T) {
	for _, kind := range []string{"own source", "other source", "same output", "symlink source", "hardlink source", "parent alias", "overwrite duplicate"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")
			put(t, a, []byte("original a"))
			put(t, b, []byte("original b"))
			inputs := []string{a, b}
			dests := map[string]string{a: filepath.Join(dir, "out-a.png"), b: filepath.Join(dir, "out-b.png")}
			overwrite := false
			switch kind {
			case "own source":
				dests[a] = a
			case "other source":
				dests[a] = b
			case "same output":
				dests[b] = dests[a]
			case "symlink source":
				if err := os.Symlink(a, dests[a]); err != nil {
					t.Skip(err)
				}
			case "hardlink source":
				if err := os.Link(a, dests[a]); err != nil {
					t.Skip(err)
				}
			case "parent alias":
				realDir, alias := filepath.Join(dir, "real"), filepath.Join(dir, "alias")
				if err := os.Mkdir(realDir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(realDir, alias); err != nil {
					t.Skip(err)
				}
				dests[a] = filepath.Join(realDir, "new", "result.png")
				dests[b] = filepath.Join(alias, "new", "result.png")
			case "overwrite duplicate":
				if err := os.Remove(b); err != nil {
					t.Fatal(err)
				}
				if err := os.Link(a, b); err != nil {
					t.Skip(err)
				}
				overwrite = true
				dests[a] = a
				dests[b] = b
			}
			_, err := Plan(inputs, imgopter.FormatPNG, func(src string, _ imgopter.Format) string { return dests[src] }, overwrite)
			if err == nil {
				t.Fatal("unsafe plan accepted")
			}
			data, err := os.ReadFile(a)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, []byte("original a")) {
				t.Fatal("source changed during planning")
			}
		})
	}
}

func TestPlanResolvesFormatsAndAllowsOwnOverwrite(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		header []byte
		format imgopter.Format
	}{
		{"jpeg", []byte{255, 216, 255}, imgopter.FormatJPEG},
		{"png", []byte("\x89PNG\r\n\x1a\n"), imgopter.FormatPNG},
		{"webp", []byte("RIFF\x00\x00\x00\x00WEBP"), imgopter.FormatWebP},
		{"gif", []byte("GIF89a"), imgopter.FormatPNG},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(dir, tc.name)
			put(t, src, tc.header)
			plan, err := Plan([]string{src}, imgopter.FormatAuto, func(path string, f imgopter.Format) string {
				if f != tc.format {
					t.Fatalf("format %v, want %v", f, tc.format)
				}
				return path
			}, true)
			if err != nil {
				t.Fatal(err)
			}
			if plan[0].Dest != src {
				t.Fatalf("dest %s", plan[0].Dest)
			}
		})
	}
}

func TestAtomicWriteFailurePreservesDestination(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[exists], func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "out.png")
			if exists {
				put(t, path, []byte("original"))
			}
			failure := errors.New("disk full")
			err := writeAtomic(path, func(w io.Writer) error {
				if _, err := w.Write([]byte("partial")); err != nil {
					t.Fatal(err)
				}
				return failure
			})
			if !errors.Is(err, failure) {
				t.Fatalf("error %v", err)
			}
			data, err := os.ReadFile(path)
			if exists && (err != nil || string(data) != "original") {
				t.Fatalf("original changed: %q, %v", data, err)
			}
			if !exists && !os.IsNotExist(err) {
				t.Fatalf("failed output created: %v", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if exists {
				want = 1
			}
			if len(entries) != want {
				t.Fatalf("staging file leaked: %v", entries)
			}
		})
	}
}

func TestAtomicWriteReplacesWithoutFollowingAliases(t *testing.T) {
	for _, kind := range []string{"existing", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			source, dest := filepath.Join(dir, "source"), filepath.Join(dir, "dest")
			put(t, source, []byte("source"))
			switch kind {
			case "existing":
				put(t, dest, []byte("old"))
			case "symlink":
				if err := os.Symlink(source, dest); err != nil {
					t.Skip(err)
				}
			case "hardlink":
				if err := os.Link(source, dest); err != nil {
					t.Skip(err)
				}
			}
			if err := WriteFile(dest, []byte("replacement")); err != nil {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(dest)
			if string(data) != "replacement" {
				t.Fatalf("output %q", data)
			}
			data, _ = os.ReadFile(source)
			if string(data) != "source" {
				t.Fatalf("source changed: %q", data)
			}
			info, err := os.Stat(dest)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("permissions %o", info.Mode().Perm())
			}
		})
	}
}

func TestAtomicRenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dest")
	err := writeAtomic(path, func(w io.Writer) error {
		// A conflicting destination appears while the staging file is being written.
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := w.Write([]byte("replacement"))
		return err
	})
	if err == nil {
		t.Fatal("expected rename failure")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "dest" || !entries[0].IsDir() {
		t.Fatalf("unexpected files: %v", entries)
	}
}

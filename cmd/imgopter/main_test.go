package main

import (
	"bytes"
	"image"
	"image/png"
	"imgopter"
	"os"
	"path/filepath"
	"testing"
)

func writePNG(t *testing.T, path string, w int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, w, 8))); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestProcessRejectsCollisionsBeforeWriting(t *testing.T) {
	for _, kind := range []string{"other input", "shared output"} {
		for _, jobs := range []int{1, 4} {
			t.Run(kind+string(rune('0'+jobs)), func(t *testing.T) {
				dir := t.TempDir()
				a, b := filepath.Join(dir, "a.png"), filepath.Join(dir, "a-opt.png")
				out := ""
				if kind == "shared output" {
					b = filepath.Join(dir, "sub", "a.png")
					out = filepath.Join(dir, "out")
				}
				originals := map[string][]byte{a: writePNG(t, a, 8), b: writePNG(t, b, 16)}
				results := process([]string{a, b}, imgopter.Options{Quality: 100}, out, "-opt", false, jobs)
				for _, r := range results {
					if r.err == nil {
						t.Fatalf("unsafe batch succeeded: %+v", r)
					}
				}
				for path, want := range originals {
					got, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("source %s modified: %v", path, err)
					}
				}
				if out != "" {
					if _, err := os.Stat(out); !os.IsNotExist(err) {
						t.Fatalf("output directory created: %v", err)
					}
				}
			})
		}
	}
}

func TestProcessWritesSafeBatchAndOverwrite(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		dir := t.TempDir()
		inputs := []string{filepath.Join(dir, "a.png"), filepath.Join(dir, "b.png")}
		for _, p := range inputs {
			writePNG(t, p, 16)
		}
		results := process(inputs, imgopter.Options{Quality: 100, MaxWidth: 8}, "", "-opt", overwrite, 2)
		for _, r := range results {
			if r.err != nil {
				t.Fatal(r.err)
			}
			f, err := os.Open(r.out)
			if err != nil {
				t.Fatal(err)
			}
			config, err := png.DecodeConfig(f)
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if err != nil {
				t.Fatal(err)
			}
			if config.Width != 8 {
				t.Fatalf("output width %d", config.Width)
			}
			if overwrite && r.out != r.in {
				t.Fatalf("overwrite output %s", r.out)
			}
		}
	}
}

// Command imgopter optimizes images from the command line.
//
//	imgopter [flags] <file-or-dir...>
package main

import (
	"flag"
	"fmt"
	"imgopter"
	"imgopter/internal/output"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var usage = `Usage: imgopter [flags] <file-or-dir...>

Decode, resize and re-encode images for size optimization.
Directory args are scanned for supported images (jpg, jpeg, png, gif,
bmp, tif, tiff, webp); file args are used as-is. Arguments may be globs.

Flags:
  -q, --quality int      target quality 1-100 (default 80)
      --max-width int    max output width px (0 = unlimited)
      --max-height int   max output height px (0 = unlimited)
      --mode string      fit|crop (default "fit")
      --format string    auto|jpeg|png|webp (default "auto")
  -o, --out string       output file (single input only) or directory
      --suffix string    suffix before extension for outputs (default "-opt")
      --overwrite        overwrite input files in place
  -r, --recursive        scan directory args recursively
      --jobs int         parallel workers (default %d)
`

func main() {
	var (
		quality   int
		maxWidth  int
		maxHeight int
		jobs      int
		modeS     string
		formatS   string
		out       string
		suffix    string
		overwrite bool
		recursive bool
	)
	flag.IntVar(&quality, "q", 80, "target quality 1-100")
	flag.IntVar(&quality, "quality", 80, "target quality 1-100")
	flag.IntVar(&maxWidth, "max-width", 0, "max output width px")
	flag.IntVar(&maxHeight, "max-height", 0, "max output height px")
	flag.StringVar(&modeS, "mode", "fit", "fit|crop")
	flag.StringVar(&formatS, "format", "auto", "auto|jpeg|png|webp")
	flag.StringVar(&out, "o", "", "output file or directory")
	flag.StringVar(&out, "out", "", "output file or directory")
	flag.StringVar(&suffix, "suffix", "-opt", "suffix before extension")
	flag.BoolVar(&overwrite, "overwrite", false, "overwrite inputs")
	flag.BoolVar(&recursive, "r", false, "scan dirs recursively")
	flag.BoolVar(&recursive, "recursive", false, "scan dirs recursively")
	flag.IntVar(&jobs, "jobs", runtime.NumCPU(), "parallel workers")
	flag.Usage = func() { fmt.Fprintf(os.Stderr, usage, runtime.NumCPU()) }
	flag.Parse()

	mode, err := imgopter.ParseMode(modeS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	format, err := imgopter.ParseFormat(formatS)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	inputs, expandErrs := expandArgs(flag.Args(), recursive)
	failures := 0
	for _, err := range expandErrs {
		failures++
		fmt.Fprintln(os.Stderr, err)
	}
	if len(inputs) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if jobs < 1 {
		jobs = 1
	}

	opts := imgopter.Options{
		Quality:   quality,
		MaxWidth:  maxWidth,
		MaxHeight: maxHeight,
		Mode:      mode,
		Format:    format,
	}
	results := process(inputs, opts, out, suffix, overwrite, jobs)

	var ok, totIn, totOut int
	for _, r := range results {
		if r.err != nil {
			failures++
			fmt.Fprintf(os.Stderr, "%s: failed: %v\n", r.in, r.err)
			continue
		}
		ok++
		totIn += r.res.InBytes
		totOut += r.res.OutBytes
		fmt.Printf("%s → %s  %s → %s  (%+.1f%%)\n",
			r.in, r.out, humanBytes(r.res.InBytes), humanBytes(r.res.OutBytes), -r.res.Savings())
	}
	if ok > 0 {
		fmt.Printf("Optimized %d/%d images · %s → %s (%+.1f%%)\n",
			ok, len(results), humanBytes(totIn), humanBytes(totOut), -savings(totIn, totOut))
	} else {
		fmt.Printf("Optimized 0/%d images\n", len(results))
	}

	if failures > 0 {
		os.Exit(1)
	}
}

// expandArgs resolves args (files, dirs, globs) into an ordered, deduplicated
// input list. Missing paths and unreadable dirs come back as errors.
func expandArgs(args []string, recursive bool) ([]string, []error) {
	var inputs []string
	var errs []error
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			inputs = append(inputs, p)
		}
	}
	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil || len(matches) == 0 {
			matches = []string{arg}
		}
		for _, m := range matches {
			info, err := os.Stat(m)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", m, err))
				continue
			}
			if !info.IsDir() {
				add(m)
				continue
			}
			if recursive {
				err := filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					if !d.IsDir() && imgopter.IsSupportedInput(p) {
						add(p)
					}
					return nil
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", m, err))
				}
				continue
			}
			entries, err := os.ReadDir(m)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", m, err))
				continue
			}
			for _, e := range entries {
				p := filepath.Join(m, e.Name())
				if !e.IsDir() && imgopter.IsSupportedInput(p) {
					add(p)
				}
			}
		}
	}
	return inputs, errs
}

type outcome struct {
	in  string
	out string
	res *imgopter.Result
	err error
}

// process optimizes inputs on a bounded worker pool and returns one outcome
// per input, in input order.
func process(inputs []string, o imgopter.Options, out, suffix string, overwrite bool, jobs int) []outcome {
	results := make([]outcome, len(inputs))
	plans, err := output.Plan(inputs, o.Format, func(in string, f imgopter.Format) string {
		return outPath(in, f, len(inputs) == 1, out, suffix, overwrite)
	}, overwrite)
	if err != nil {
		for i, in := range inputs {
			results[i] = outcome{in: in, err: err}
		}
		return results
	}
	queue := make(chan int)
	var wg sync.WaitGroup
	for range min(max(1, jobs), len(inputs)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queue {
				results[i] = processOne(plans[i], o)
			}
		}()
	}
	for i := range inputs {
		queue <- i
	}
	close(queue)
	wg.Wait()
	return results
}

func processOne(job output.Job, o imgopter.Options) outcome {
	res, err := imgopter.OptimizeFile(job.Source, o)
	if err == nil {
		err = output.WriteFile(job.Dest, res.Data)
	}
	return outcome{in: job.Source, out: job.Dest, res: res, err: err}
}

// outPath resolves where the optimized form of in gets written.
func outPath(in string, f imgopter.Format, single bool, out, suffix string, overwrite bool) string {
	if overwrite {
		return in
	}
	stem := strings.TrimSuffix(filepath.Base(in), filepath.Ext(in))
	var dest string
	switch {
	case out == "":
		dest = filepath.Join(filepath.Dir(in), stem+suffix+f.Ext())
	case single && isImageExt(filepath.Ext(out)):
		dest = out
	default:
		dest = filepath.Join(out, stem+suffix+f.Ext())
	}
	return dest
}

func isImageExt(ext string) bool {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

func savings(in, out int) float64 {
	if in <= 0 {
		return 0
	}
	return (1 - float64(out)/float64(in)) * 100
}

func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

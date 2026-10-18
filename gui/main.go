// Command gui is the imgopter desktop app: a native-UI window for picking
// images, tuning the optimization and watching the savings.
package main

import (
	"bytes"
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"imgopter"

	"github.com/disintegration/imaging"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/gen2brain/webp"
)

// File row states.
const (
	stPending = iota
	stWorking
	stDone
	stFailed
)

var formats = []string{"Auto", "JPEG", "PNG", "WebP"}

// imageExts are the file dialog's extensions, without dots.
func imageExts() []string {
	exts := imgopter.SupportedInputExt()
	out := make([]string, len(exts))
	for i, e := range exts {
		out[i] = strings.TrimPrefix(e, ".")
	}
	return out
}

// fileItem is one queued image and its result.
type fileItem struct {
	path, name      string
	inSize, outSize int64
	origW, origH    int // decoded source dims, 0 until known
	outW, outH      int
	status          int
	err             string
	thumb           *ui.Bitmap
}

// app is the window's model; it changes on the main thread, in the view or
// through Window.Update.
type app struct {
	win  *mygo.Window
	svc  ui.Services
	have map[string]bool // paths already listed

	files     []*fileItem
	list      ui.ListState
	quality   float64 // 1–100
	maxW      float64 // 0 = unconstrained
	maxH      float64
	modeIdx   int    // 0 fit, 1 crop
	format    string // formats[i]
	outDir    string // "" = beside each source
	suffix    string
	overwrite bool

	running  bool
	done     int           // files processed in the current run
	total    int           // files queued for it
	note     string        // toasted once set
	lastNote string        // the last run's outcome, in the status bar
	runDone  chan struct{} // closed when a run ends; nil or closed at rest
}

func main() {
	a := &app{quality: 80, format: formats[0], suffix: "-opt"}
	mygo.App.WhenReady(func() {
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "imgopter",
			Width: 920, Height: 600,
			MinWidth: 680, MinHeight: 420,
			StateKey: "main",
			Content:  ui.View(a.view),
		})
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// options maps the controls onto an Optimize run. Overwrite keeps the input
// codec: the format control is off and the path, extension included, is kept.
func (a *app) options() imgopter.Options {
	o := imgopter.Options{
		Quality:   int(math.Round(a.quality)),
		MaxWidth:  int(math.Round(a.maxW)),
		MaxHeight: int(math.Round(a.maxH)),
	}
	if a.modeIdx == 1 {
		o.Mode = imgopter.ModeCrop
	}
	switch a.format {
	case "JPEG":
		o.Format = imgopter.FormatJPEG
	case "PNG":
		o.Format = imgopter.FormatPNG
	case "WebP":
		o.Format = imgopter.FormatWebP
	}
	if a.overwrite {
		o.Format = imgopter.FormatAuto
	}
	return o
}

// update runs fn on the main thread, repainting the window.
func (a *app) update(fn func()) {
	if a.win != nil {
		a.win.Update(fn)
		return
	}
	fn()
}

// progress mirrors the run on the taskbar.
func (a *app) progress(v float64) {
	if a.win != nil {
		a.win.SetProgressBar(mygo.ProgressBar{Value: v})
	}
}

func (a *app) view(c *ui.Context) {
	a.svc = c.Services()
	t := c.Theme()
	if a.note != "" {
		c.Toast(a.note)
		a.note = ""
	}
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).
			BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
			if ui.PrimaryButton(c, "Add images…").Clicked() {
				a.pick(false)
			}
			if ui.Button(c, "Add folder…").Clicked() {
				a.pick(true)
			}
			ui.Spacer(c)
			if ui.Button(c, "Clear list").Disabled(a.running || len(a.files) == 0).Clicked() {
				a.files = nil
				a.have = nil
			}
		})
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.controls(c)
			a.fileArea(c)
		})
		a.statusBar(c)
	})
}

// controls is the left column of settings and the run button.
func (a *app) controls(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Width(296).Shrink(0).Padding(16).Gap(16).
		BorderWidth(0, 1, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, "Settings").Bold()
		ui.Field(c, fmt.Sprintf("Quality — %d", int(math.Round(a.quality))), func() {
			ui.Slider(c, &a.quality, 1, 100).Step(1)
		})
		ui.Field(c, "Max width (0 = any)", func() {
			ui.NumberInput(c, &a.maxW, 0, 100000, 50)
		})
		ui.Field(c, "Max height (0 = any)", func() {
			ui.NumberInput(c, &a.maxH, 0, 100000, 50)
		})
		ui.Field(c, "Resize mode", func() {
			ui.Segmented(c, &a.modeIdx, "Fit", "Crop")
		})
		ui.Field(c, "Format", func() {
			ui.Select(c, &a.format, formats).Disabled(a.overwrite)
		})
		ui.Divider(c)
		ui.Text(c, "Output").Bold()
		ui.Field(c, "Folder (empty = source)", func() {
			ui.Row(c).Gap(8).Children(func() {
				ui.TextInput(c, &a.outDir).Grow(1).MinWidth(0).
					Placeholder("Same folder").Disabled(a.overwrite)
				if ui.Button(c, "Browse…").Disabled(a.overwrite).Clicked() {
					a.pickOutDir()
				}
			})
		})
		ui.Field(c, "Filename suffix", func() {
			ui.TextInput(c, &a.suffix).Disabled(a.overwrite)
		})
		ui.Checkbox(c, &a.overwrite, "Overwrite originals")
		ui.Divider(c)
		if ui.PrimaryButton(c, "Optimize").Disabled(a.running || len(a.files) == 0).Clicked() {
			a.start()
		}
		if a.running {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Progress(c, float64(a.done)/float64(max(a.total, 1))).Grow(1)
				ui.Spinner(c)
				ui.Textf(c, "%d/%d", a.done, a.total).FontSize(12).TextColor(t.TextMuted)
			})
		}
	})
}

// fileArea is the right side: a drop zone when empty, the list otherwise.
func (a *app) fileArea(c *ui.Context) {
	t := c.Theme()
	area := ui.Column(c).Grow(1).MinWidth(0)
	if files := area.DroppedFiles(); files != nil {
		go a.ingest(files)
	}
	over := area.FileDragOver()
	area.Children(func() {
		if len(a.files) == 0 {
			zone := ui.Column(c).Grow(1).Margin(20).Radius(10).Gap(4).
				Border(2, t.Border).BorderStyle(ui.BorderDashed).
				Center().Cursor(ui.CursorPointer)
			if over {
				zone.Border(2, t.Accent)
			}
			zone.Children(func() {
				ui.Text(c, "Drop images here").FontSize(16).Bold()
				ui.Text(c, "or click to browse").TextColor(t.TextMuted)
			})
			if zone.Clicked() {
				a.pick(false)
			}
			return
		}
		remove := -1
		a.list.Key = func(i int) any { return a.files[i].path }
		ui.List(c, &a.list, len(a.files), func(i int) {
			if i >= len(a.files) {
				return
			}
			f := a.files[i]
			row := ui.Row(c).Padding(6, 14).Gap(12).AlignItems(ui.Center)
			if row.Hovered() {
				row.Background(t.Surface)
			}
			row.Children(func() {
				if f.thumb != nil {
					ui.Image(c, f.thumb).Size(44, 44).Fit(ui.Contain).Shrink(0)
				} else {
					ui.Box(c).Size(44, 44).Radius(6).Background(t.Surface).Border(1, t.Border).Shrink(0)
				}
				ui.Column(c).Grow(1).MinWidth(0).Children(func() {
					ui.Text(c, f.name).SingleLine()
					sub := humanBytes(f.inSize)
					if f.origW > 0 {
						sub = fmt.Sprintf("%d×%d · %s", f.origW, f.origH, humanBytes(f.inSize))
					}
					ui.Text(c, sub).FontSize(12).TextColor(t.TextMuted).SingleLine()
				})
				a.rowStatus(c, f)
				if ui.Button(c, "✕").Disabled(a.running).Tooltip("Remove from list").Clicked() {
					remove = i
				}
			})
		}).Grow(1)
		if remove >= 0 {
			delete(a.have, a.files[remove].path)
			a.files = slices.Delete(a.files, remove, remove+1)
		}
	})
	if over {
		area.Border(2, t.Accent)
	}
}

// rowStatus is a row's right side: the result, or where it is.
func (a *app) rowStatus(c *ui.Context, f *fileItem) {
	t := c.Theme()
	switch f.status {
	case stWorking:
		ui.Spinner(c).Shrink(0)
	case stDone:
		pct := 0.0
		if f.inSize > 0 {
			pct = (1 - float64(f.outSize)/float64(f.inSize)) * 100
		}
		col := t.Success
		if pct < 0 {
			col = t.Warning
		}
		ui.Textf(c, "→ %s (−%.0f%%)", humanBytes(f.outSize), pct).
			FontSize(12).TextColor(col).SingleLine()
	case stFailed:
		ui.Text(c, "failed").FontSize(12).TextColor(t.Danger).Tooltip(f.err)
	default:
		ui.Text(c, "pending").FontSize(12).TextColor(t.TextMuted)
	}
}

// statusBar is the bottom line of totals and the last run's note.
func (a *app) statusBar(c *ui.Context) {
	t := c.Theme()
	var in, out int64
	for _, f := range a.files {
		in += f.inSize
		if f.status == stDone {
			out += f.outSize
		}
	}
	ui.Row(c).Padding(7, 14).Gap(12).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		label := fmt.Sprintf("%d files · %s", len(a.files), humanBytes(in))
		if len(a.files) == 1 {
			label = fmt.Sprintf("1 file · %s", humanBytes(in))
		}
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
		if out > 0 {
			pct := (1 - float64(out)/float64(in)) * 100
			ui.Textf(c, "→ %s (−%.0f%%)", humanBytes(out), pct).
				FontSize(12).TextColor(t.Success)
		}
		ui.Spacer(c)
		if a.running {
			ui.Textf(c, "Optimizing %d/%d…", a.done, a.total).
				FontSize(12).TextColor(t.TextMuted)
		} else if a.lastNote != "" {
			ui.Text(c, a.lastNote).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// pick opens the file or folder dialog on a goroutine (it blocks) and adds
// what was chosen.
func (a *app) pick(dir bool) {
	go func() {
		opts := mygo.OpenDialogOptions{
			Parent:   a.win,
			Title:    "Add images",
			Multiple: true,
			Filters: []mygo.FileFilter{
				{Name: "Images", Extensions: imageExts()},
			},
		}
		if dir {
			opts.Title = "Add a folder of images"
			opts.Directory = true
		}
		paths, err := mygo.Dialog.Open(opts)
		if err != nil {
			return
		}
		a.ingest(paths)
	}()
}

// pickOutDir asks where optimized files go.
func (a *app) pickOutDir() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent:    a.win,
			Title:     "Output folder",
			Directory: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		a.update(func() { a.outDir = paths[0] })
	}()
}

// ingest expands folders to the images in them (top level only), filters to
// supported inputs and loads the rest.
func (a *app) ingest(paths []string) {
	var files []string
	for _, p := range paths {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			entries, err := os.ReadDir(p)
			if err != nil {
				continue
			}
			for _, e := range entries {
				fp := filepath.Join(p, e.Name())
				if !e.IsDir() && imgopter.IsSupportedInput(fp) {
					files = append(files, fp)
				}
			}
			continue
		}
		if imgopter.IsSupportedInput(p) {
			files = append(files, p)
		}
	}
	a.load(files)
}

// pending is a decoded item waiting for its bitmap, which must be made on
// the main thread.
type pending struct {
	item  *fileItem
	small image.Image
}

// load decodes files and their thumbnails off the main thread, then commits
// the new items.
func (a *app) load(paths []string) {
	var items []pending
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		it := &fileItem{path: p, name: filepath.Base(p), inSize: info.Size()}
		pd := pending{item: it}
		if img := decodeImage(p); img != nil {
			it.origW, it.origH = img.Bounds().Dx(), img.Bounds().Dy()
			pd.small = imaging.Fit(img, 96, 96, imaging.Lanczos)
		}
		items = append(items, pd)
	}
	a.update(func() {
		if a.have == nil {
			a.have = map[string]bool{}
		}
		for _, pd := range items {
			if a.have[pd.item.path] {
				continue
			}
			a.have[pd.item.path] = true
			if pd.small != nil {
				pd.item.thumb = ui.NewBitmap(pd.small)
			}
			a.files = append(a.files, pd.item)
		}
	})
}

// decodeImage reads and decodes path, orienting JPEGs as their EXIF says.
// WebP goes first: image.Decode does not know it.
func decodeImage(path string) image.Image {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if imgopter.SniffFormat(data) == imgopter.FormatWebP {
		img, err := webp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil
		}
		return img
	}
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil
	}
	return img
}

// start runs the queue sequentially off the main thread.
func (a *app) start() {
	if a.running || len(a.files) == 0 {
		return
	}
	opts := a.options()
	queue := slices.Clone(a.files)
	a.running, a.done, a.total = true, 0, len(queue)
	a.runDone = make(chan struct{})
	for _, f := range queue {
		f.status, f.err, f.outSize, f.outW, f.outH = stPending, "", 0, 0, 0
	}
	a.progress(0.01)
	runDone := a.runDone
	go func() {
		var ok, failed int
		var totIn, totOut int64
		for i, f := range queue {
			a.update(func() { f.status = stWorking })
			res, err := imgopter.OptimizeFile(f.path, opts)
			var dest string
			if err == nil {
				dest, err = a.destFor(f, res)
			}
			if err == nil {
				err = os.MkdirAll(filepath.Dir(dest), 0o755)
			}
			if err == nil {
				err = os.WriteFile(dest, res.Data, 0o644)
			}
			st, errStr := stDone, ""
			var outSize int64
			var outW, outH, origW, origH int
			if err != nil {
				st, errStr = stFailed, err.Error()
				failed++
			} else {
				outSize = int64(res.OutBytes)
				outW, outH = res.Width, res.Height
				origW, origH = res.OrigWidth, res.OrigHeight
				ok++
				totIn += int64(res.InBytes)
				totOut += int64(res.OutBytes)
			}
			a.update(func() {
				f.status, f.err = st, errStr
				f.outSize, f.outW, f.outH = outSize, outW, outH
				if origW > 0 {
					f.origW, f.origH = origW, origH
				}
				a.done = i + 1
				a.progress(float64(a.done) / float64(a.total))
			})
		}
		note := fmt.Sprintf("Optimized %d of %d — %s → %s", ok, len(queue), humanBytes(totIn), humanBytes(totOut))
		if failed > 0 {
			note += fmt.Sprintf(" (%d failed)", failed)
		}
		a.update(func() {
			a.running = false
			a.note, a.lastNote = note, note
			a.progress(0) // clears it
			close(runDone)
		})
	}()
}

// destFor resolves where f's optimized form is written: f's own path when
// overwriting, else outDir-or-source-dir / stem + suffix + resolved ext.
func (a *app) destFor(f *fileItem, res *imgopter.Result) (string, error) {
	if a.overwrite {
		return f.path, nil
	}
	dir := a.outDir
	if dir == "" {
		dir = filepath.Dir(f.path)
	}
	stem := strings.TrimSuffix(f.name, filepath.Ext(f.name))
	dest := filepath.Join(dir, stem+a.suffix+res.Format.Ext())
	inAbs, err1 := filepath.Abs(f.path)
	destAbs, err2 := filepath.Abs(dest)
	if err1 == nil && err2 == nil && inAbs == destAbs {
		return "", fmt.Errorf("%s would overwrite the source; check Overwrite or pick a suffix", f.name)
	}
	return dest, nil
}

// humanBytes formats a size as the CLI does.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

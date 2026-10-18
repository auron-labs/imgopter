# imgopter

Make images smaller, resize photos, and convert between formats with a desktop app or the command line. Process a single image or a whole folder, then see how much space you saved.

imgopter reads JPEG, PNG, WebP, GIF, BMP, and TIFF. It saves JPEG, PNG, and WebP, keeping your originals by default.

## Get started

You'll need Go 1.27.1 or newer. Run these commands from the project directory.

### Desktop app

```sh
go run ./gui
```

1. Click **Add images…** or **Add folder…** to choose your images.
2. Choose the quality, size, and output format. Leave the defaults to try it out.
3. Click **Optimize** to process the images and see the results.

By default, the app saves each result beside its original with `-opt` added to the name: `photo.jpg` becomes `photo-opt.jpg`. Choose an output folder to keep the results together.

On Linux, the desktop app needs GTK 3 and WebKitGTK installed.

### Command line

Build the CLI, then pass it an image:

```sh
go build -o build/imgopter ./cmd/imgopter
./build/imgopter photo.jpg
```

This creates `photo-opt.jpg` and prints the original size, new size, and percentage saved. On Windows, build with `-o build/imgopter.exe` and run `./build/imgopter.exe`.

## Common tasks

Put options before the image paths. You can pass multiple files, a folder, or a quoted glob such as `'photos/*.jpg'`.

### Resize a photo

Fit a photo within 1600 × 1200 pixels while keeping its proportions:

```sh
./build/imgopter --max-width 1600 --max-height 1200 photo.jpg
```

Smaller images keep their size. Set just one dimension to limit only the width or height.

For a square thumbnail, use crop mode:

```sh
./build/imgopter --mode crop --max-width 400 --max-height 400 photo.jpg
```

This crops from the center to exactly 400 × 400 pixels and can enlarge small images.

### Optimize a folder

Process a folder and its subfolders, saving the results into `optimized/`:

```sh
./build/imgopter --recursive --out optimized photos/
```

Output files go directly into `optimized/`; subfolders are not preserved. If two images would produce the same filename, imgopter stops the batch before writing. Process those folders separately or choose different output directories.

### Convert to WebP or JPEG

```sh
./build/imgopter --format webp photo.jpg
./build/imgopter --format jpeg --out converted.jpg photo.png
```

The first command creates `photo-opt.webp`. The second saves the result as `converted.jpg`. Without `--format`, JPEG, PNG, and WebP keep their format; GIF, BMP, and TIFF become PNG.

### Adjust quality

The default quality is 80. Try a lower value for smaller files, or a higher value to retain more detail:

```sh
./build/imgopter --quality 65 photo.jpg
```

For PNG, quality below 100 reduces the color palette. Use `--quality 100` to preserve pixel colors:

```sh
./build/imgopter --quality 100 screenshot.png
```

Run `./build/imgopter --help` for all options, including output naming and parallel workers.

## What happens to your images

Originals stay in place unless you choose **Overwrite originals** in the app or pass `--overwrite` to the CLI. Back them up before using that option. To convert GIF, BMP, or TIFF, save a separate output instead of overwriting the input.

Re-encoding removes metadata, including EXIF, color profiles, and comments. Animated images become a single frame. These changes also apply at quality 100.

Some images get larger after re-encoding. imgopter shows the size change and saves the result either way. Existing output files can be replaced, so choose a fresh output folder if you want to keep earlier results.

## Development

[mise](https://mise.jdx.dev/) installs the pinned development tools:

```sh
mise install
mise run test
mise run build:cli
mise run dev
```

Use `mise run build:gui` to package the desktop app for your current platform. Build outputs go into `build/`.

See [CONTRIBUTING.md](CONTRIBUTING.md) for formatting, linting, and other checks.

## License

[MIT](LICENSE).

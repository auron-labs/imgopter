# Contributing

Install the tools with `mise install`, then run `mise exec -- hk install --mise` to install the repository hooks locally.

Before submitting a change, run:

```sh
mise run test:race
mise run vet
mise run lint
mise run build:cli
```

Use `mise run fmt` to format Go files. For desktop changes, also run `mise run dev` and exercise the affected controls. The Go suite includes headless UI tests; it does not replace checking a real window on the affected platform.

The existing lint configuration currently reports findings in the Go sources. CI runs race tests, vet, and builds; lint remains a local check until that baseline is resolved.

Use conventional commit messages such as `fix: preserve image orientation` or `feat: add an output option`. Release automation uses these messages to determine version changes. Keep behavior changes covered by focused tests, especially output collisions, image fidelity, and preservation of originals.

Release-please manages versions, tags, and changelogs. Its release PR keeps `mygo.json` and `.release-please-manifest.json` in sync; merging that PR creates the tag and a draft release, then starts CLI and desktop packaging through `.github/workflows/release.yml`.

MyGo builds Linux archives and Debian packages, a universal macOS disk image, and a Windows installer on native runners. Each runner verifies every expected package before handing its artifacts to the publishing job. GoReleaser waits for all runners, builds the CLI archives, and uploads the desktop packages, CLI archives, and shared `checksums.txt` to the draft. It publishes the release only after every upload succeeds, keeping the asset set complete before GitHub makes the release immutable. A failed build, package check, or upload leaves the release unpublished; rerun the failed jobs to finish it. Never publish the draft manually before its assets are complete.

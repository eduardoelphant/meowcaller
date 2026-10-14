# meowcaller, Elphant fork

Fork of [purpshell/meowcaller](https://github.com/purpshell/meowcaller) (MIT, see `LICENSE`),
used by the Elphant fork of GOWA
([eduardoelphant/go-whatsapp-web-multidevice](https://github.com/eduardoelphant/go-whatsapp-web-multidevice)).

## Branches and versions

- `elphant` is the working branch and the default branch. It starts at upstream `27a3c6b`, the
  last upstream commit on the official `go.mau.fi/whatsmeow` before upstream moved to the
  `polymorfa/hypermeow` fork.
- Releases are tags `vX.Y.Z-elphant.N`. GOWA keeps importing `github.com/purpshell/meowcaller`
  and points at a tag of this fork with a `replace` in its `go.mod`. The module path in this
  repository's `go.mod` stays the upstream one on purpose.
- `main` mirrors upstream and is not used.

## Changes on top of upstream

| Change | Why |
|---|---|
| FFT twiddle cache (upstream PR #38, same change as PR #30, cherry-picked with its author) | MLow encoder about 2.2x faster on the benchmark |
| Dither before encoding (`engine_media.go`) | Silence after speech drove the MLow encoder into subnormal floats, about 3x slower per frame; a live call went from about 48% to about 11.5% of a core |
| Opus mode: `SetCodec("opus")` (`codec_mode.go`, `engine.go`, `engine_media.go`) | Clears capability 31 (`use_mlow_codec_v1`) and sends `voip_settings` turning MLow off in the accept, so both sides use standard Opus (PT120, 16 kHz, 60 ms). Modelled on whatsapp-rust PR #1050. About 4% of a core per call, tested with Android and WhatsApp Web |
| `opus/`: minimal cgo binding to libopus | Opus encode and decode for the mode above; needs `libopus` and `pkg-config` at build time |

## Syncing with upstream

Bring upstream changes through a `sync/<date>` branch merged into `elphant` with a merge commit,
as GOWA does. Upstream `main` imports `github.com/polymorfa/hypermeow`; rewrite those imports back
to `go.mau.fi/whatsmeow` in the sync branch and build against the whatsmeow version GOWA uses.
If upstream comes to need something only `hypermeow` has, decide case by case.

## Build and test

```sh
# macOS: brew install opus pkg-config   Debian/Ubuntu: apt-get install libopus-dev pkg-config
GOTOOLCHAIN=auto go vet ./... && GOTOOLCHAIN=auto go test ./...
```

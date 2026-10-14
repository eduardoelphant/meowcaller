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
| Relay fanout (upstream [PR #26](https://github.com/purpshell/meowcaller/pull/26) by pedrobartolini, commit `340c889` only, cherry-picked with its author) | Phones move their uplink to another offered relay after election; binding one relay lost the caller's audio. Binds every offered `te2`, merges receives behind an (SSRC, seq) replay filter, answers latency probes only for offered relays |
| Media on the offer (upstream [PR #43](https://github.com/purpshell/meowcaller/pull/43) by michsantoz, cherry-picked with its author) | Answering more than about 2 s after the offer lost inbound audio; media now starts while ringing, and an early `mute_v2` and first RTP are applied on Answer. Test imports switched from `hypermeow` to `go.mau.fi/whatsmeow` |
| Stale offer gate (upstream PR #26 commit `3494d98` by pedrobartolini, cherry-picked with its author) | Offers replayed from the offline queue older than 90 s (`e` attribute) are ignored |
| Fixes on top of PR #26 and #43 (`engine_media.go`, `engine_relay_fanout.go`, `engine.go`) | Abandoned or cancelled relay connects close their channel; relays connect in parallel; the fanout closes as soon as the call ends; latency probes are echoed only for relays the fanout bound; media that ends while ringing can be restarted by Answer; binding-success goes back on the relay that asked |
| Instrumentation (`engine.go`, `engine_media.go`, `engine_relay_log.go`) | Info logs for the offer's relay list (`relay_name`, `is_fna`, `c2r_rtt`; never tokens), relay updates, latency probes echoed or skipped, `mute_v2` toggles, and a 10 s media counter line (peer RTP raw and after the replay filter, peer RTCP, our RTP sent, last peer SR `packet_count`) |
| `WithOfferFilter` option (`logging.go`, `engine.go`) | Lets GOWA leave an offer (1:1 or group) to another stack: no preaccept, no handler, no state |
| Raw call controls only for known calls (`engine.go`, `engine_group_control.go`) | `group_update`, `enc_rekey`, `waiting_room_update`, `user_action`, `screen_share` and `video` for a call-id the engine does not hold (filtered by `WithOfferFilter`, ended, or not ours) get whatsmeow's plain ack and nothing else; group rekeys check the call before the Signal decrypt |
| `opus/` stub for `!cgo` builds (`opus/opus_nocgo.go`) | `CGO_ENABLED=0` builds compile; there `SetCodec("opus")` keeps MLow and returns `ErrOpusUnavailable`, and media falls back to MLow if an Opus constructor fails |

## Versions

- `v0.1.0-elphant.1`: FFT twiddle cache, dither, Opus mode and the libopus binding.
- `v0.2.0-elphant.1`: relay fanout and stale offer gate (PR #26), media on the offer (PR #43),
  fixes on top of both, instrumentation, `WithOfferFilter` and the `!cgo` Opus stub.

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

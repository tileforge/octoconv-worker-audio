# octoconv-worker-audio

Audio worker for OctoConv. One process serves one approved manifest over Worker Protocol v2.

The process reads `AUDIO_PROTOCOL_CORE_URL`, `AUDIO_PROTOCOL_CLIENT_CERT`, `AUDIO_PROTOCOL_CLIENT_KEY`, `AUDIO_PROTOCOL_CLIENT_CA`, `AUDIO_PROTOCOL_WORKER_ID`, and `AUDIO_PROTOCOL_MANIFEST_PATH`. It refuses to start when `DATABASE_URL` or `REDIS_ADDR` is set. `AUDIO_WORKER_CONCURRENCY` must be unset or `1`. Hello and pull advertise one slot, and one conversion runs at a time.

`schema/manifest-audio-convert.json` is capability `octoconv.convert.audio`. It names the thirty-six pairs: sources mp3, wav, m4a, ogg, mp4, mov, avi, mkv, and webm, each to txt, srt, vtt, and json.

`ffmpeg` and `whisper-cli` must be on `PATH`. The model file defaults to `/models/ggml-base.bin`. `AUDIO_MODEL_PATH` overrides that path. `AUDIO_THREADS` overrides the thread count passed to `whisper-cli`. The process applies Landlock and refuses to start when Landlock is unavailable. `AUDIO_SANDBOX_DISABLED=true` is the only switch that runs the engines without the ruleset. The job directory is the only writable path. Read-only paths are `/lib`, `/usr/lib`, `/usr/local/lib`, `/etc/ld.so.cache`, `/proc/cpuinfo`, `/sys/devices/system/cpu/online`, `/sys/devices/system/cpu/possible`, the `ffmpeg`, `ffprobe`, and `whisper-cli` binaries, and the model file. This process does not call `ffprobe`. The outside fixture is `/var/tmp`. A missing binary or a helper exit of 78 is not a denial.

On 2026-11-18 the local image `octoconv-audio:confinement-measure` `sha256:9eff16a2e4745b7c780f19957fc9607242b630d2742f15bbfa676229d80f2634`, linux/arm64, OrbStack kernel 7.2.12, ffmpeg 5.1.9-0+deb12u1, whisper.cpp 1.9.1, logged `audio sandbox proven` with a 32078-byte normalized WAV and a 532-byte transcript, and wrote no output for the same bytes outside every grant. Approving the manifest is what sends jobs to this process. The manifest is not approved.

`language` is `auto`, `en`, `ru`, `es`, `fr`, or `de`. An empty language uses `-l auto`. `translate` true adds `-tr`. Any other key fails before either program runs.

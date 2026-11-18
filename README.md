# octoconv-worker-audio

Audio worker for OctoConv. One process serves one approved manifest over Worker Protocol v2.

The process reads `AUDIO_PROTOCOL_CORE_URL`, `AUDIO_PROTOCOL_CLIENT_CERT`, `AUDIO_PROTOCOL_CLIENT_KEY`, `AUDIO_PROTOCOL_CLIENT_CA`, `AUDIO_PROTOCOL_WORKER_ID`, and `AUDIO_PROTOCOL_MANIFEST_PATH`. It refuses to start when `DATABASE_URL` or `REDIS_ADDR` is set. `AUDIO_WORKER_CONCURRENCY` must be unset or `1`. Hello and pull advertise one slot, and one conversion runs at a time.

`schema/manifest-audio-convert.json` is capability `octoconv.convert.audio`. It names the thirty-six pairs: sources mp3, wav, m4a, ogg, mp4, mov, avi, mkv, and webm, each to txt, srt, vtt, and json.

`ffmpeg` and `whisper-cli` must be on `PATH`. The model file defaults to `/models/ggml-base.bin`. `AUDIO_MODEL_PATH` overrides that path. `AUDIO_THREADS` overrides the thread count passed to `whisper-cli`. This process does not apply Landlock. The in-repo audio worker does. Do not move audio jobs onto this process until that confinement is measured.

`language` is `auto`, `en`, `ru`, `es`, `fr`, or `de`. An empty language uses `-l auto`. `translate` true adds `-tr`. Any other key fails before either program runs.

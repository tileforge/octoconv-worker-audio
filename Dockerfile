FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/audio-worker ./cmd/audio-worker

FROM debian:bookworm-slim AS whisper-build
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential \
      cmake \
      git \
      ca-certificates \
      curl \
 && rm -rf /var/lib/apt/lists/*
# The v1.9.1 tag is mutable. This is the commit it pointed at on 2026-07-18.
ARG WHISPER_COMMIT=f049fff95a089aa9969deb009cdd4892b3e74916
RUN git clone --depth 1 --branch v1.9.1 \
      https://github.com/ggml-org/whisper.cpp.git /whisper \
 && git -C /whisper checkout --detach "${WHISPER_COMMIT}" \
 && [ "$(git -C /whisper rev-parse HEAD)" = "${WHISPER_COMMIT}" ]
WORKDIR /whisper
# GGML_NATIVE compiles for the build CPU and SIGILLs on a host without those instructions.
RUN cmake -B build -DGGML_NATIVE=OFF -DCMAKE_BUILD_TYPE=Release \
 && cmake --build build -j --target whisper-cli --config Release
RUN mkdir -p /models \
 && curl -L --fail --retry 5 --retry-delay 5 \
      -o /models/ggml-base.bin \
      https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin \
 && echo "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe  /models/ggml-base.bin" | sha256sum -c -

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      ffmpeg \
 && rm -rf /var/lib/apt/lists/*
COPY --from=whisper-build /whisper/build/bin/whisper-cli /usr/local/bin/whisper-cli
# cmake writes libwhisper and libggml next to the binary and does not set RPATH.
COPY --from=whisper-build /whisper/build/bin/*.so* /usr/local/lib/
RUN ldconfig
COPY --from=whisper-build /models/ggml-base.bin /models/ggml-base.bin
COPY --from=build /out/audio-worker /usr/local/bin/audio-worker
USER nobody
ENTRYPOINT ["/usr/local/bin/audio-worker"]

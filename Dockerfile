FROM golang:1.24-bookworm AS builder

WORKDIR /app

# Install build deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    make \
    python3 \
    python3-venv \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

# Cache Go modules
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source and build
COPY . .
RUN make build

# ---- Runtime ----
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-venv \
    python3-pip \
    ca-certificates \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/bin/aeon /usr/local/bin/aeon
COPY workspace/ /app/workspace/

RUN useradd -m -s /bin/bash aeon
USER aeon

ENTRYPOINT ["aeon"]
CMD ["serve"]

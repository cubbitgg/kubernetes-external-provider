FROM golang:1.25-bookworm AS builder

WORKDIR /workspace

COPY commonlib/ ./commonlib/
COPY cmd-drivers/ ./cmd-drivers/
COPY node-scanner/ ./node-scanner/

RUN printf 'go 1.25.0\n\nuse (\n\t./commonlib\n\t./cmd-drivers\n\t./node-scanner\n)\n' > go.work

RUN cd node-scanner && go mod download
RUN cd node-scanner && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /node-scanner ./cmd

# The node-scanner needs:
# - lsblk (from util-linux) to discover block devices
# - mount/umount (from mount) for bind mounts
# - mkfs.ext4 (from e2fsprogs) to format raw disks
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends \
        util-linux \
        mount \
        e2fsprogs \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /node-scanner /usr/local/bin/node-scanner
ENTRYPOINT ["/usr/local/bin/node-scanner"]

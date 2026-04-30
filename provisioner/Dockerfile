FROM golang:1.25-bookworm AS builder

WORKDIR /workspace

COPY commonlib/ ./commonlib/
COPY cmd-drivers/ ./cmd-drivers/
COPY provisioner/ ./provisioner/

RUN printf 'go 1.25.0\n\nuse (\n\t./commonlib\n\t./cmd-drivers\n\t./provisioner\n)\n' > go.work

RUN cd provisioner && go mod download
RUN cd provisioner && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /provisioner ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /provisioner /provisioner
ENTRYPOINT ["/provisioner"]

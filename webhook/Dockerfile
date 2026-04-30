FROM golang:1.25-bookworm AS builder

WORKDIR /workspace

COPY commonlib/ ./commonlib/
COPY cmd-drivers/ ./cmd-drivers/
COPY webhook/ ./webhook/

RUN printf 'go 1.25.0\n\nuse (\n\t./commonlib\n\t./cmd-drivers\n\t./webhook\n)\n' > go.work

RUN cd webhook && go mod download
RUN cd webhook && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /webhook ./cmd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /webhook /webhook
ENTRYPOINT ["/webhook"]

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY home-assistant/go.mod ./
COPY home-assistant/*.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /northumberland-bins .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /northumberland-bins /northumberland-bins
USER 65532:65532
ENTRYPOINT ["/northumberland-bins"]

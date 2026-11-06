# --- build -------------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relay .

# --- runtime -----------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/relay /relay

ENV RELAY_ADDR=:8080 \
    RELAY_DATA=/data

VOLUME ["/data"]
EXPOSE 8080

ENTRYPOINT ["/relay"]

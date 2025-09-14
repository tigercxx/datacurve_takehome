FROM golang:1.24.7 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN --mount=type=cache,target="/root/.cache/go-build" CGO_ENABLED=0 go build -o /out/api ./cmd/api && \
    CGO_ENABLED=0 go build -o /out/worker ./cmd/worker && \
    CGO_ENABLED=0 go build -o /out/smoke ./cmd/smoke

FROM gcr.io/distroless/base-debian12
COPY --from=build /out/api /app/api
COPY --from=build /out/worker /app/worker
COPY --from=build /out/smoke /app/smoke
EXPOSE 8000
CMD ["/app/api"]

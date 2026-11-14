FROM golang:1.22-alpine AS build
WORKDIR /src

# Сначала зависимости: этот слой кэшируется и пересобирается, только если изменились go.mod/go.sum.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/app ./cmd/app

# Финальный образ без shell и пакетного менеджера: только бинарник.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/app /app
USER nonroot:nonroot
ENV GIN_MODE=release
EXPOSE 8080
ENTRYPOINT ["/app"]

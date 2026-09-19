# Build multi-stage do binário único `noto` (serve, worker e migrate).
FROM golang:1.27 AS build
WORKDIR /src

# dependências primeiro, para o cache de camada sobreviver a mudanças de código
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/noto ./cmd/noto

# Imagem final mínima: sem shell, sem gerenciador de pacotes, sem root.
# (o fuso IANA vem embutido no binário via time/tzdata)
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/noto /noto
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/noto"]
CMD ["serve"]

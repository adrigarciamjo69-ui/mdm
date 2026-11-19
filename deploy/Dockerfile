# Imagen del servidor MDM. Se construye desde la raíz del repositorio:
#   docker build -f deploy/Dockerfile -t mdm-server .

# 1. Consola web
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# 2. Servidor y agentes
FROM golang:1.23-bookworm AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=1.0.0
RUN make build VERSION=${VERSION} \
 && mkdir -p /out/downloads /out/data/files \
 && cp dist/mdm-server /out/ \
 && cp dist/mdm-agent-* scripts/install.sh scripts/install.ps1 /out/downloads/ \
 && cd /out/downloads && sha256sum mdm-agent-* > SHA256SUMS

# 3. Imagen final: solo el binario, sin intérprete de órdenes
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /out/mdm-server /mdm-server
COPY --from=go /out/downloads /downloads
# Carpeta de los instaladores subidos, propiedad del usuario sin privilegios:
# el volumen con nombre hereda estos permisos la primera vez que se monta.
COPY --from=go --chown=65532:65532 /out/data /data
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/mdm-server"]
CMD ["serve"]

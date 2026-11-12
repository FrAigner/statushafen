# syntax=docker/dockerfile:1

# Alle Basis-Images sind auf Version und Digest festgelegt (nachvollziehbare Builds).
# Beim Anheben beides ändern (Digest siehe backend/dockerfile_images_test.go); die Tests
# dort prüfen Digest-Pflicht und Lebenszyklen.

# ---- Stage 1: Frontend (Vue.js) bauen ----
FROM node:24-alpine@sha256:8a32c8cf649a49915a66e81e7a609223c708d0adabf288ee3f4077d765542f85 AS frontend-builder
WORKDIR /app/frontend

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

COPY frontend/ ./
RUN npm run build

# ---- Stage 2: Backend (Go) bauen ----
FROM golang:1.27.0-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS backend-builder
WORKDIR /app/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/server .

# ---- Stage 3: Minimales Laufzeit-Image ----
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
WORKDIR /app

# ca-certificates: nötig für ausgehende HTTPS-Aufrufe (Google/GitHub OAuth-Endpunkte)
RUN apk add --no-cache ca-certificates && \
    addgroup -S app && adduser -S app -G app

COPY --from=backend-builder /app/bin/server ./server
COPY --from=frontend-builder /app/frontend/dist ./static
# Lizenz und Hinweise zu Drittsoftware (u. a. SIL Open Font License der Schriften) liegen im Image bei
COPY LICENSE THIRD_PARTY_NOTICES.md /licenses/

USER app

# Render setzt PORT zur Laufzeit (i.d.R. 10000); main.go liest os.Getenv("PORT")
# und fällt lokal/ohne Vorgabe auf 8080 zurück
ENV PORT=8080
EXPOSE 8080

# /healthz prüft auch die Datenbank. Das Image hat kein curl; wget kommt von busybox.
# Die Shell-Form liest PORT zur Laufzeit.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD wget -q -O /dev/null "http://127.0.0.1:${PORT:-8080}/healthz" || exit 1

ENTRYPOINT ["./server"]

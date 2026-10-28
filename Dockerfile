# One image: the Go binary (before-yes serve) and the built web app.
# Build from the repository root: docker build -t before-yes .

# Web app: the Svelte build, static files into web/build.
FROM node:26-alpine AS web
RUN npm install -g pnpm@11.28.5
WORKDIR /src/web
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile --ignore-scripts
COPY web/ ./
RUN pnpm exec vite build

# Go binary: pure Go (modernc sqlite), so CGO is off and the binary is static.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/before-yes ./cmd/before-yes
# The data directory is created here so the named volume inherits the
# non-root owner on first mount.
RUN mkdir -p /out/data && chown 65532:65532 /out/data

# Runtime: distroless static (CA certificates, no shell), non-root uid 65532.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/before-yes /app/before-yes
COPY --from=web /src/web/build /app/web
COPY testdata/demo-cards /app/testdata/demo-cards
COPY deploy/before-yes.prod.toml /app/before-yes.toml
COPY --from=build --chown=65532:65532 /out/data /data
ENV BEFORE_YES_CONFIG=/app/before-yes.toml
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/app/before-yes"]
CMD ["serve"]

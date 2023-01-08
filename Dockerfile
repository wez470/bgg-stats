FROM alpine:latest
WORKDIR /app
COPY server /app/
COPY templates /app/
ENTRYPOINT ["/app/server"]
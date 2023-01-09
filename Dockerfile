FROM alpine:latest
WORKDIR /app
COPY server /app/
COPY templates /app/templates/
ENTRYPOINT ["/app/server"]
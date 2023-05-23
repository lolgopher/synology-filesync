# build stage
FROM golang:1.20 as builder

ENV CGO_ENABLED=0 \
    BUILD_TAG="unknown" \
    BUILD_TIME="unknown" \
    GIT_HASH="unknown"

RUN apt-get update -y
# RUN apt-get install -y upx

WORKDIR /build
COPY . ./

RUN go get
RUN go mod tidy
RUN go mod download

RUN go test
RUN if [ "$BUILD_TAG" = "unknown" ]; then export BUILD_TAG=dev; fi \
    && if [ "$BUILD_TIME" = "unknown" ]; then export BUILD_TIME=$(date '+%Y-%m-%d_%H:%M:%S_%Z'); fi \
    && if [ "$GIT_HASH" = "unknown" ]; then export GIT_HASH=$(git rev-parse --short HEAD); fi \
    && LDFLAGS="-s -X main.buildStamp=${BUILD_TIME} -X main.gitHash=${GIT_HASH} -X main.buildTag=${BUILD_TAG}" \
    && go build -a -ldflags "${LDFLAGS}" -o app .

RUN strip /build/app
# RUN upx -q -9 /build/app

# ---
FROM alpine

ENV SYNOLOGY_IP="" \
    SYNOLOGY_PORT="" \
    SYNOLOGY_ID="" \
    SYNOLOGY_PW="" \
    SYNOLGOY_PATH="" \
    REMOTE_IP="" \
    REMOTE_PORT="" \
    REMOTE_ID="" \
    REMOTE_PW="" \
    REMOTE_PATH="" \
    LOCAL_PATH=""

COPY --from=builder /build/app .
COPY --from=builder /build/entrypoint.sh .

RUN chmod +x entrypoint.sh

ENTRYPOINT ["./entrypoint.sh"]

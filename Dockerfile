ARG version
ARG commit

FROM golang:1.13

WORKDIR /app

ENV version $version
ENV commit $commit

COPY go.mod .
COPY go.sum .

RUN go mod download
RUN go mod verify

COPY *.go .

RUN go build -ldflags "-X main.gVersion=$version -X main.gCommit=$commit"

FROM scratch

COPY --from=0 /app/kcm /kcm

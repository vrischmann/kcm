FROM golang:1.15

ARG version
ARG commit

WORKDIR /app

COPY go.mod .
COPY go.sum .

RUN go mod download
RUN go mod verify

COPY *.go .

RUN go build -ldflags "-X main.gVersion=$version -X main.gCommit=$commit"

FROM scratch

COPY --from=0 /app/kcm /kcm

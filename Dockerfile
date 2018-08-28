FROM golang:1.11-alpine3.8
RUN apk update && apk add vim tree lsof bash git gcc musl-dev
ENV GOPATH=/home/decred/go
ENV PATH=/go/bin:/usr/local/go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$GOPATH/bin
ENV DCRSRC_PATH=$GOPATH/src/github.com/decred/dcrdata/
ENV GO111MODULE=on
RUN adduser -s /bin/bash -D -h /home/decred decred && chown -R decred:decred /home/decred
WORKDIR $DCRSRC_PATH
RUN chown -R decred:decred $GOPATH 
# since we might be rebulding often we need to cache this module layer
# otherwise docker will detect changes everytime and re-download everything again
COPY go.* $DCRSRC_PATH
RUN go mod download 
COPY . $DCRSRC_PATH
RUN chown -R decred:decred $GOPATH 
USER decred
RUN go build
CMD /bin/bash

ENTRYPOINT ./dcrdata
# Note: when building the --squash flag is an experimental feature as of Docker 18.06
# docker build --squash -t decred/dcrdata .
# running
# docker run -ti --rm decred/dcrdata
# or if attaching source volume and developing interactively
#  docker run -ti --entrypoint=/bin/bash -v ${PWD}:${PWD}:/home/decred/go/src/github.com/decred/dcrdata --rm decred/dcrdata
#dcrdata-1

FROM golang:1.10 

LABEL description="Dcrdata"
LABEL version="1.0"
LABEL maintainer vracek@protonmail.com

ENV TERM linux
ENV USER build

# create user
RUN adduser --disabled-password --gecos '' $USER

# update base distro & install  build tooling
ENV DEBIAN_FRONTEND noninteractive

RUN apt-get update && \
    apt-get install -qy rsync 

# create directory for build artifacts, adjust user permissions 
RUN mkdir /release && \
    chown $USER /release

# create directory to get source from
RUN mkdir /src && \
    chown $USER /src && \
    mkdir -p /go/src/github.com/decred/dcrdata && \
    mkdir -p /go/src/github.com/decred/dcrwallet && \
    mkdir -p /go/src/github.com/decred/dcrctl && \
    mkdir -p /go/src/github.com/decred/dcrrpcclient && \
    chown -R $USER /go/src

#switch user
USER $USER
ENV HOME /home/$USER

#Get deps
ENV DEP_TAG v0.4.1
ENV GLIDE_TAG v0.13.1
ENV GOMETALINTER_TAG v2.0.5

WORKDIR /go/src
RUN go get -v github.com/Masterminds/glide && \
    cd /go/src/github.com/Masterminds/glide && \
    git checkout $GLIDE_TAG && \
    make build && \
    mv glide `which glide` && \
    go get -v github.com/alecthomas/gometalinter && \
    cd /go/src/github.com/alecthomas/gometalinter && \
    git checkout $GOMETALINTER_TAG && \
    go install && \
    gometalinter --install && \
    go get -v github.com/golang/dep && \
    cd /go/src/github.com/golang/dep && \
    git checkout $DEP_TAG && \ 
    go install -i 

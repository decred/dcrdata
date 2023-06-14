FROM golang:1.20 as daemon

COPY . /go/src
WORKDIR /go/src/cmd/dcrdata
RUN env GO111MODULE=on go build -v

FROM node:lts as gui

WORKDIR /root
COPY ./cmd/dcrdata /root
RUN npm install
RUN npm run build

FROM golang:1.20
WORKDIR /
COPY --from=daemon /go/src/cmd/dcrdata/dcrdata /dcrdata
COPY --from=daemon /go/src/cmd/dcrdata/views /views
COPY --from=gui /root/public /public

EXPOSE 7777
CMD [ "/dcrdata" ]

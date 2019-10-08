FROM golang:1.13 as daemon

COPY . /go/src
WORKDIR /go/src
RUN env GO111MODULE=on go build

FROM node:lts as gui

WORKDIR /root
COPY . /root
RUN npm install
RUN npm run build

FROM golang:1.13
WORKDIR /
COPY --from=daemon /go/src/dcrdata /dcrdata
COPY --from=daemon /go/src/views /views
COPY --from=gui /root/public /public

EXPOSE 7777
CMD [ "/dcrdata" ]

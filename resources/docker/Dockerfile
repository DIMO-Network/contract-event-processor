FROM golang:1.21 AS build

RUN useradd -u 10001 dimo

WORKDIR /go/src/github.com/DIMO-Network/contract-event-processor/
COPY . /go/src/github.com/DIMO-Network/contract-event-processor/

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOFLAGS=-mod=vendor

RUN ls
RUN go mod tidy
RUN go mod vendor
RUN make install

FROM busybox AS package

LABEL maintainer="DIMO <hello@dimo.zone>"

WORKDIR /

COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /go/src/github.com/DIMO-Network/contract-event-processor/target/bin/contract-event-processor .
COPY --from=build /go/src/github.com/DIMO-Network/contract-event-processor/migrations ./migrations
COPY --from=build /go/src/github.com/DIMO-Network/contract-event-processor/abi ./abi
COPY --from=build /go/src/github.com/DIMO-Network/contract-event-processor/config-dev.yaml .
COPY --from=build /go/src/github.com/DIMO-Network/contract-event-processor/config-prod.yaml .

USER dimo

EXPOSE 8080
EXPOSE 8888

CMD /contract-event-processor

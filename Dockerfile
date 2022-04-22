FROM golang:1.18.0
WORKDIR /servicecreator
ADD . .
RUN go mod download && CGO_ENABLED=0 go build

FROM scratch
WORKDIR /servicecreator
COPY --from=0 servicecreator .
ENTRYPOINT [ "./servicecreator" ]

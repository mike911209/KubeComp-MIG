FROM golang:1.22

WORKDIR /go/src/kubecomp
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o=bin/reconfig ./main.go

FROM alpine

COPY --from=0 /go/src/kubecomp/bin/reconfig /bin/reconfig

WORKDIR /bin
CMD ["reconfig"]
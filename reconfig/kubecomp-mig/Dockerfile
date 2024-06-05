FROM nvidia/cuda:12.4.1-cudnn-devel-ubuntu22.04

RUN apt-get update && apt-get install -y wget
RUN wget https://go.dev/dl/go1.22.3.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.22.3.linux-amd64.tar.gz 

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /go/src/kubecomp

COPY . .

RUN GOOS=linux GOARCH=amd64 go build -o bin/reconfig ./cmd/reconfig

FROM nvidia/cuda:12.4.1-runtime-ubuntu22.04

COPY --from=0 /go/src/kubecomp/bin/reconfig /bin/reconfig

WORKDIR /bin
CMD ["reconfig"]
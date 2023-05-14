FROM golang:1.17-alpine as binary

ENV GO111MODULE=on \
    CGO_ENABLED=1

WORKDIR /build

# Copy and download dependency using go mod
COPY go.mod .
COPY go.sum .
RUN go mod download

# Install Compile plug

RUN apk -U add ca-certificates
RUN apk update && apk upgrade && apk add pkgconf git bash build-base sudo
RUN git clone https://github.com/edenhill/librdkafka.git && cd librdkafka && ./configure --prefix /usr && make && make install

COPY . .

RUN go build -tags musl --ldflags "-extldflags -static" -o moff-social ./cmd/moff-social/*


COPY --from=tarampampam/curl:7.78.0 /bin/curl /bin/curl

ENTRYPOINT ["/build/moff-social"]

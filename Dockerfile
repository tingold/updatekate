FROM golang:1.8-alpine
MAINTAINER tingold
RUN mkdir -p /go/src/github.com/tingold/updatekate

# Add all source code
ADD . /go/src/github.com/tingold/updatekate
# Run the Go installer
RUN go install github.com/tingold/updatekate

# Indicate the binary as our entrypoint
ENTRYPOINT /go/bin/updatekate

# Expose your port
EXPOSE 8888

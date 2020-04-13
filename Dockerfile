FROM golang:1.14.2-alpine
ENV GO111MODULE on
ENV CGO_ENABLED 0
WORKDIR /tmp/websubhub
COPY . .
#RUN ["go", "vet", "modfin-websubhub"]
RUN ["go", "test", "."]
RUN ["go", "build", "-o", "/usr/local/sbin/hub", "."]
WORKDIR /usr/local/sbin
RUN ["rm", "-rf", "/tmp/websubhub"]
CMD ["/usr/local/sbin/hub"]

FROM golang:latest as build

ENV IMPORT_PATH github.com/src-d/github-reminder
ADD . /go/src/$IMPORT_PATH

WORKDIR /go/src/$IMPORT_PATH 

RUN CGO_ENABLED=0 go install -a -ldflags '-extldflags "-static"' .

FROM alpine
RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2
COPY --from=build /go/bin/github-reminder /github-reminder
ENTRYPOINT [ "/github-reminder" ]

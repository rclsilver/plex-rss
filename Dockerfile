###############
# base images #
###############
FROM golang:1.23-alpine AS build
FROM scratch AS final


########################
# build Go application #
########################
FROM build AS app-build
COPY . /go/src/github.com/rclsilver/plex-rss
WORKDIR /go/src/github.com/rclsilver/plex-rss
RUN apk add --no-cache make git bash && \
    make build && \
    echo 'nobody:x:65534:65534:Nobody:/:' > /tmp/passwd


#####################
# build final image #
#####################
FROM final
WORKDIR /

COPY --from=app-build /tmp/passwd /etc/passwd
COPY --from=app-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=app-build /go/src/github.com/rclsilver/plex-rss/plex-rss /plex-rss

USER 65534
EXPOSE 8080/tcp
EXPOSE 8081/tcp
ENTRYPOINT [ "/plex-rss" ]

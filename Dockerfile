FROM alpine
RUN apk add --no-cache tzdata dumb-init
ENV TZ Europe/Oslo
ADD f5elastic /
ADD GeoLite2-City.mmdb /
ENTRYPOINT ["/usr/bin/dumb-init", "--"]
CMD ["/f5elastic"]
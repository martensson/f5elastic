FROM alpine
RUN apk add --no-cache tzdata
ENV TZ Europe/Oslo
ADD f5elastic /
ADD GeoLite2-City.mmdb /
CMD ["/f5elastic"]

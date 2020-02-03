# f5elastic

A fast system tool for receiving high volumes of traffic logs sent from F5 LTM load balancers and indexes these requests inside ES.

It is today battle tested in a production environment handling loads of 500k req/min on a single host.

Features

* Works with Elasticsearch 7.x
* Support for clusters of ES nodes, for distribution of indexing load.
* Health checks for each ES node is enabled by default.
* Uses bulk indexing for better performance with large amount of requests.
* GeoIP lookup of each request, adds long/lat, city, and country for each client.
* Allows to obfuscate client ip if needed.
* ...

## Getting started

1. Create a Request Logging profile on your BigIP, and add the following Response Settings template (set HSL Protocol to TCP):
```
<34> $CLIENT_IP || $HTTP_METHOD || ${Host} || $HTTP_URI || $HTTP_STATCODE || $RESPONSE_SIZE || ${Referer} || ${User-Agent} || $SERVER_IP:$SERVER_PORT || $VIRTUAL_POOL_NAME || $VIRTUAL_NAME
```

2. Download compiled binary from `https://github.com/martensson/f5elastic/releases` or `go get github.com/martensson/f5elastic`

3. Download the latest Maxmind GeoLite2-City or GeoIP2-City db.

4. Edit `f5elastic.toml` (check example in repo)

5. Apply the ES template inside the repo. (optional but recommended)

6. Run `f5elastic -f /path/to/f5elastic.toml`

7. Take a cup of coffee and make some nice dashboards inside Kibana :)

## Extra recommendations

Use [curator](https://www.elastic.co/guide/en/elasticsearch/client/curator/current/index.html) to create and rotate daily indexes.

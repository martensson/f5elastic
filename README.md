# f5elastic

A fast system tool for receiving high volumes of traffic logs sent from F5 LTM load balancers and indexes these requests inside ES.

It is today battle tested in a production environment handling loads of 500k req/min on a single host.

## Getting started

1. Enable the following irule on your virtual servers:
```tcl
when CLIENT_ACCEPTED {
    set hsl [HSL::open -proto TCP -pool f5elastic_tcp]
    set clientip [IP::remote_addr]
}

when HTTP_REQUEST {
    set method [HTTP::method]
    set uri [HTTP::uri]
    set host [HTTP::host]
    set ua ""
    set referer ""
    if { [HTTP::header exists "User-Agent"] } {
        set ua [HTTP::header User-Agent]
    }
    if { [HTTP::header exists "Referer"] } {
        set referer [HTTP::header Referer]
    }
}
when HTTP_RESPONSE {
    set syslogtime [clock format [clock seconds] -format "%b %d %H:%M:%S"]
    set contentlength 0
    if { [HTTP::header exists "Content-Length"] } {
        set contentlength [HTTP::header "Content-Length"]
    }
    set status [HTTP::status]
    set node [LB::server addr]:[LB::server port]
    set pool [LB::server pool]
    set virtual [virtual name]
    HSL::send $hsl "<34>$syslogtime f5 request: $clientip || $method || $host || $uri || $status || $contentlength || $referer || $ua || $node || $pool || $virtual\n"
}
```

2. `go get github.com/martensson/f5elastic`

3. edit `f5elastic.toml` (check example in repo)

4. apply the ES template inside the repo. (optional but recommended)

5. run `f5elastic -f /path/to/f5elastic.toml`

6. Take a cup of coffee and make some nice dashboards inside Kibana :)

## Extra recommendations

Use [curator](https://www.elastic.co/guide/en/elasticsearch/client/curator/current/index.html) to create and rotate daily indexes.

# f5elastic

* A fast application for receiving high volumes of traffic logs sent from F5 LTM load balancers and indexes each request inside Elasticsearch. 

It is today used in a production environment handling loads of 500k req/min on
a single host.

Enable the following irule on your virtual servers:

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



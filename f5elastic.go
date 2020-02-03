package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/signal"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	lru "github.com/hashicorp/golang-lru"
	"github.com/olivere/elastic/v7"
	"github.com/oschwald/geoip2-golang"
	"gopkg.in/mcuadros/go-syslog.v2"
)

type Request struct {
	Client        string `json:"client"`
	Method        string `json:"method"`
	Host          string `json:"host"`
	Uri           string `json:"uri"`
	Status        int    `json:"status,string"`
	Contentlength int    `json:"content-length,string"`
	Referer       string `json:"referer"`
	Useragent     string `json:"user-agent"`
	Node          string `json:"node"`
	Pool          string `json:"pool"`
	Virtual       string `json:"virtual"`
	City          string `json:"city"`
	Country       string `json:"country"`
	Location      string `json:"location"`
	Timestamp     string `json:"timestamp"`
}

type Config struct {
	Address    string
	Port       string
	Nodes      []string
	Index      string
	Workers    int
	Bulk       int
	Buffer     int
	Timeout    int
	Geoip      string
	Salt       string
	CidrIgnore []string
}

type Worker struct {
	Work     chan string
	Indexer  *elastic.BulkProcessor
	Esclient *elastic.Client
}

func NewWorker(work chan string, c *elastic.Client) Worker {
	// Setup a bulk processor
	indexer, err := c.BulkProcessor().
		Workers(1).
		BulkActions(config.Bulk).                                   // commit if # requests
		FlushInterval(time.Duration(config.Timeout) * time.Second). // commit at Timeout
		Do(context.Background())
	if err != nil {
		log.Fatalln(err)
	}
	return Worker{
		Work:     work,
		Indexer:  indexer,
		Esclient: c}
}

func (w Worker) NewRequest(msg string) (Request, error) {
	msgparts := strings.Split(msg, " || ")
	var request Request
	if len(msgparts) != 11 {
		err := errors.New("Error parsing raw message: " + msg)
		return request, err
	}
	request.Client = msgparts[0]
	request.Method = msgparts[1]
	request.Host = msgparts[2]
	request.Uri = msgparts[3]
	if s, err := strconv.Atoi(msgparts[4]); err == nil {
		request.Status = s
	}
	// requests without valid status code will be ignored. Happens when we use http::retry.
	if request.Status < 100 || request.Status > 511 {
		err := errors.New("Error invalid status code: " + strconv.Itoa(request.Status) + " : " + request.Host + request.Uri)
		return request, err
	}
	if s, err := strconv.Atoi(msgparts[5]); err == nil {
		request.Contentlength = s
	}
	request.Referer = msgparts[6]
	request.Useragent = msgparts[7]
	request.Node = msgparts[8]
	request.Pool = path.Base(msgparts[9])
	request.Virtual = path.Base(msgparts[10])
	// use LRU cache
	var record *geoip2.City
	if val, ok := geocache.Get(request.Client); ok {
		record = val.(*geoip2.City)
	} else {
		ip := net.ParseIP(request.Client)
		var err error
		record, err = geodb.City(ip)
		if err != nil {
			log.Printf("Error parsing client ip %s: %s\n", request.Client, err)
		}
		geocache.Add(request.Client, record)
	}
	if record.Location.Longitude != 0 && record.Location.Latitude != 0 {
		request.City = record.City.Names["en"]
		request.Country = record.Country.Names["en"]
		request.Location = strconv.FormatFloat(record.Location.Latitude, 'f', 6, 64) + "," + strconv.FormatFloat(record.Location.Longitude, 'f', 6, 64)
	}
	request.Timestamp = time.Now().UTC().Format("2006-01-02T15:04:05Z")
	if config.Salt != "" {
		ip := net.ParseIP(request.Client)
		var found bool
		for _, cidr := range config.CidrIgnore {
			_, ipnet, _ := net.ParseCIDR(cidr)
			if ipnet.Contains(ip) {
				found = true
			}
		}
		if !found {
			// hash client ip with secret salt.
			if val, ok := hashcache.Get(request.Client); ok {
				request.Client = val.(string)
			} else {
				h := sha256.New()
				h.Write([]byte(request.Client + config.Salt))
				hexhash := hex.EncodeToString(h.Sum(nil))[0:16]
				hashcache.Add(request.Client, hexhash)
				request.Client = hexhash
			}
		}
	}
	return request, nil
}

func (w Worker) Start() {
	go func() {
		wg.Add(1)
		defer wg.Done()
		for {
			select {
			case m, ok := <-w.Work:
				if !ok {
					w.Indexer.Flush()
					return
				}
				request, err := w.NewRequest(m)
				if err != nil {
					log.Println(err)
					continue
				}
				reqIndex := elastic.NewBulkIndexRequest().Index(config.Index + time.Now().Format("-2006-01-02")).Type("_doc").Id("").Doc(request)
				w.Indexer.Add(reqIndex)
			}
		}
	}()
}

var wg sync.WaitGroup
var geocache *lru.Cache
var hashcache *lru.Cache
var geodb *geoip2.Reader
var config Config

func main() {
	configtoml := flag.String("f", "f5elastic.toml", "Path to config.")
	flag.Parse()
	file, err := ioutil.ReadFile(*configtoml)
	if err != nil {
		log.Fatal(err)
	}
	err = toml.Unmarshal(file, &config)
	if err != nil {
		log.Fatal("Problem parsing config: ", err)
	}
	// init syslog server
	channel := make(syslog.LogPartsChannel)
	handler := syslog.NewChannelHandler(channel)
	server := syslog.NewServer()
	server.SetFormat(syslog.RFC3164)
	server.SetHandler(handler)
	server.ListenUDP(config.Address + ":" + config.Port)
	server.ListenTCP(config.Address + ":" + config.Port)
	err = server.Boot()
	if err != nil {
		log.Fatal(err)
	}
	// init geoip2 database
	// http://geolite.maxmind.com/download/geoip/database/GeoLite2-City.mmdb.gz
	geodb, err = geoip2.Open(config.Geoip)
	if err != nil {
		log.Fatal(err)
	}
	defer geodb.Close()
	// init our lru cache of geodb
	geocache, _ = lru.New(100000)
	// init our lru cache of sha256 hashes
	hashcache, _ = lru.New(100000)
	// init our elastic client
	c, err := elastic.NewClient(
		elastic.SetURL(config.Nodes...),
		elastic.SetSniff(false),
		elastic.SetHealthcheckInterval(10*time.Second),
		elastic.SetMaxRetries(5))
	if err != nil {
		log.Fatal(err)
	}
	// A buffered channel that we can send work requests on.
	var workqueue = make(chan string, config.Buffer)
	// lets start our desired workers.
	for i := 0; i < config.Workers; i++ {
		worker := NewWorker(workqueue, c)
		worker.Start()
	}
	go func(channel syslog.LogPartsChannel) {
		for logParts := range channel {
			m := logParts["content"].(string)
			if m != "" {
				select {
				case workqueue <- m: // Put request in the channel unless it is full
				default:
					log.Println("Workqueue channel is full. Discarding request.")
				}
			}
		}
	}(channel)

	sigchan := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigchan, os.Interrupt)
	go func() {
		for _ = range sigchan {
			// stop our server in a graceful manner
			log.Println("Stopping f5elastic")
			server.Kill()
			close(workqueue)
			wg.Wait()
			log.Println("Finished.")
			done <- true
		}
	}()
	log.Println("Starting f5elastic")
	server.Wait()
	<-done
}

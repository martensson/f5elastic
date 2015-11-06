package main

import (
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

	"gopkg.in/mcuadros/go-syslog.v2"
	"gopkg.in/olivere/elastic.v2"

	"github.com/BurntSushi/toml"
	"github.com/hashicorp/golang-lru"
	"github.com/oschwald/geoip2-golang"
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
	Address string
	Port    string
	Nodes   []string
	Index   string
	Workers int
	Bulk    int
	Buffer  int
	Timeout int
	Geoip   string
}

type Worker struct {
	Work     chan string
	Indexer  *elastic.BulkService
	Esclient *elastic.Client
}

func NewWorker(work chan string, c *elastic.Client) Worker {
	indexer := c.Bulk()
	return Worker{
		Work:     work,
		Indexer:  indexer,
		Esclient: c}
}

func (w Worker) NewRequest(msg string) (Request, error) {
	msgparts := strings.Split(msg, " || ")
	var request Request
	if len(msgparts) != 11 {
		err := errors.New("Error parsing message:\n" + msg)
		return request, err
	}
	request.Client = msgparts[0]
	request.Method = msgparts[1]
	request.Host = msgparts[2]
	request.Uri = msgparts[3]
	if s, err := strconv.Atoi(msgparts[4]); err == nil {
		request.Status = s
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
	return request, nil
}

func (w Worker) Start() {
	go func() {
		wg.Add(1)
		defer wg.Done()
		timer := time.After(time.Second * time.Duration(config.Timeout))
		for {
			select {
			case <-timer:
				w.Indexer.Do()
				timer = time.After(time.Second * time.Duration(config.Timeout))
			case m, ok := <-w.Work:
				if !ok {
					w.Indexer.Do()
					return
				}
				request, err := w.NewRequest(m)
				if err != nil {
					log.Println(err)
					continue
				}
				reqIndex := elastic.NewBulkIndexRequest().Index(config.Index).Type("request").Id("").Doc(request)
				w.Indexer = w.Indexer.Add(reqIndex)
				// should be enough for now, but we can increase load by creating a group of workers
				if w.Indexer.NumberOfActions() >= config.Bulk {
					w.Indexer.Do()
					// reset timer
					timer = time.After(time.Second * time.Duration(config.Timeout))
				}
			}
		}
	}()
}

var wg sync.WaitGroup
var geocache *lru.Cache
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
	geocache, _ = lru.New(10000)
	// init our elastic client
	c, err := elastic.NewClient(elastic.SetURL(config.Nodes...), elastic.SetSniff(false), elastic.SetHealthcheckInterval(5*time.Second))
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

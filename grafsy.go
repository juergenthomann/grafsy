package main

import (
	"log"
	"os"
	"sync"
	"github.com/BurntSushi/toml"
	"fmt"
	"net"
	"syscall"
	"path/filepath"
)

type Config struct {
	ClientSendInterval int
	MaxMetrics int
	GraphiteAddr string // Think about multiple servers
	LocalBind string
	Log string
	MetricDir string
	RetryFile string
	SumPrefix string
	SumInterval int
	GrafsyPrefix string
	GrafsySuffix string
	AllowedMetrics string
}

const monitorMetrics  = 6
func main() {
	var conf Config
	if _, err := toml.DecodeFile("/etc/grafsy/grafsy.toml", &conf); err != nil {
		fmt.Println("Failed to parse config file", err.Error())
	}

	var wg sync.WaitGroup

	if _, err := os.Stat(filepath.Dir(conf.Log)); os.IsNotExist(err) {
		if os.MkdirAll(filepath.Dir(conf.Log), os.ModePerm) != nil {
			log.Println("Can not create logfile's dir " + filepath.Dir(conf.Log))
		}
	}

	f, err := os.OpenFile(conf.Log, os.O_RDWR | os.O_CREATE | os.O_APPEND, 0660)
	if err != nil {
		log.Println("Can not open file "+ conf.Log, err.Error())
		os.Exit(1)
	}
	lg := log.New(f, "", log.Ldate|log.Lmicroseconds|log.Lshortfile)

	graphiteAdrrTCP, err := net.ResolveTCPAddr("tcp", conf.GraphiteAddr)
	if err != nil {
		lg.Println("This is not a valid address:", err.Error())
		os.Exit(1)
	}

	/*
		Check if directories for temporary files exist
		This is especially important when your metricDir is in /tmp
	 */
	if _, err := os.Stat(conf.MetricDir); os.IsNotExist(err) {
		oldUmask := syscall.Umask(0)
		os.MkdirAll(conf.MetricDir, 0777|os.ModeSticky)
		syscall.Umask(oldUmask)
	}

	/*
		This is a main buffer
		It does not make any sense to have it too big cause metrics will be dropped during saving to file
		This buffer is ready to take maxMetric*sumInterval. Which gives you the rule, than bigger interval you have or
		amount of metric in interval, than more metric it can take in memory.
	 */
	var ch chan string = make(chan string, conf.MaxMetrics*conf.ClientSendInterval + monitorMetrics)
	/*
		This is a sum buffer. I assume it make total sense to have maximum buf = maxMetric*sumInterval.
		For example up to 10000 sums per second
	*/
	var chS chan string = make(chan string, conf.MaxMetrics*conf.SumInterval + monitorMetrics)

	/*
		Monitoring channel. Must be independent. Limited by maximum amount of monitoring metrics (6 for now)
	 */
	var chM chan string = make(chan string, monitorMetrics)

	mon := &Monitoring{
		conf, Source{},
		0,
		0,
		0,
		chM}

	cli := Client{
		conf,
		mon,
		*graphiteAdrrTCP,
		*lg,
		ch,
		chM}
	srv := Server{
		conf,
		mon,
		*lg,
		ch,
		chS}


	go srv.runServer()
	go cli.runClient()
	if conf.GrafsyPrefix != "null" && conf.GrafsySuffix != "null" {
		go mon.runMonitoring()
	}

	wg.Add(1)
	wg.Wait()
}
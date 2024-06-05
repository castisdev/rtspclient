package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"golang.org/x/sync/errgroup"
)

type config struct {
	url           string
	addr          string
	transport     string // TCP/UDP
	nStart        int
	nEnd          int
	readTimeout   time.Duration
	writeTimeout  time.Duration
	delayTimeout  time.Duration
	startInterval time.Duration
	count         int
}

func play(url, transport, id string, delayTimeout time.Duration) error {
	err := playInternal(url, transport, id, delayTimeout)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
	return err
}

type DelayChecker struct {
	mu           sync.Mutex
	lastTS       uint32
	lastT        time.Time
	checkedTS    uint32
	delayTimeout time.Duration
}

func (dc *DelayChecker) Check(pkt *rtp.Packet) {
	dc.mu.Lock()
	defer dc.mu.Unlock()

	if dc.lastTS == 0 {
		dc.lastTS = pkt.Timestamp
		dc.lastT = time.Now()
		dc.checkedTS = pkt.Timestamp
		return
	}

	if pkt.Timestamp-dc.checkedTS > 90000 {
		dc.checkedTS = pkt.Timestamp
		now := time.Now()
		diffT := now.Sub(dc.lastT).Milliseconds()
		diffTS := (pkt.Timestamp - dc.lastTS) / 90
		if diffT-int64(diffTS) > dc.delayTimeout.Milliseconds() {
			log.Printf("delayed RTP packet: %vms", diffT-int64(diffTS))
			dc.lastT = now
			dc.lastTS = pkt.Timestamp
		}
	}
}

func playInternal(url, transport, id string, delayTimeout time.Duration) error {
	tr := gortsplib.TransportUDP
	if transport == "TCP" {
		tr = gortsplib.TransportTCP
	}
	c := gortsplib.Client{
		Transport:    &tr,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
	}

	u, err := base.ParseURL(url)
	if err != nil {
		return fmt.Errorf("[%s] failed to parse url, %v", id, err)
	}

	err = c.Start(u.Scheme, u.Host)
	if err != nil {
		return fmt.Errorf("[%s] failed to start client, %v", id, err)
	}
	defer c.Close()

	desc, descRes, err := c.Describe(u)
	if err != nil {
		return fmt.Errorf("[%s] failed to describe, %v", id, err)
	}
	log.Printf("[%s] success to describe, %v", id, descRes)

	err = c.SetupAll(desc.BaseURL, desc.Medias)
	if err != nil {
		return fmt.Errorf("[%s] failed to setup, %v", id, err)
	}
	log.Printf("[%s] success to setup", id)

	dc := &DelayChecker{delayTimeout: delayTimeout}
	c.OnPacketRTPAny(func(medi *description.Media, forma format.Format, pkt *rtp.Packet) {
		dc.Check(pkt)
	})

	c.OnPacketRTCPAny(func(medi *description.Media, pkt rtcp.Packet) {
		// log.Printf("RTCP packet from media %v, type %T\n", medi, pkt)
	})

	_, err = c.Play(nil)
	if err != nil {
		return fmt.Errorf("[%s] failed to play, %v", id, err)
	}
	log.Printf("[%s] success to play", id)

	err = c.Wait()
	if err != nil {
		return fmt.Errorf("[%s] failed to play process, %v", id, err)
	}
	return nil
}

func main() {
	var cfg config

	urlUsage := "url, if url contains '{NUM}', replaces {NUM} to number (start to end)\n" +
		"(ex) url: rtsp://localhost:554/{NUM}.stream\n" +
		"start: 100\nend: 102\n" +
		"=== then use \n" +
		"rtsp://localhost:554/100.stream\n" +
		"rtsp://localhost:554/101.stream\n" +
		"rtsp://localhost:554/102.stream\n\n"
	flag.StringVar(&cfg.url, "url", "rtsp://localhost:554", urlUsage)
	flag.StringVar(&cfg.transport, "transport", "UDP", "transport type, UDP/TCP")
	flag.IntVar(&cfg.nStart, "start", 10001, "url replace {NUM} to start-end")
	flag.IntVar(&cfg.nEnd, "end", 10001, "url replace {NUM} to start-end")
	flag.DurationVar(&cfg.readTimeout, "read-timeout", 2*time.Second, "read timeout")
	flag.DurationVar(&cfg.writeTimeout, "write-timeout", 2*time.Second, "write timeout")
	flag.DurationVar(&cfg.delayTimeout, "delay-timeout", 1*time.Second, "delay timeout")
	flag.DurationVar(&cfg.startInterval, "start-interval", 10*time.Millisecond, "start session interval")
	flag.IntVar(&cfg.count, "count", 1, "play session count")

	version := flag.Bool("version", false, "print version")
	flag.Parse()

	if *version {
		fmt.Println("rtspclient version 1.0.0")
		os.Exit(0)
	}

	if cfg.transport != "UDP" && cfg.transport != "TCP" {
		fmt.Println("invalid transport")
		os.Exit(1)
	}

	if cfg.nStart > cfg.nEnd {
		fmt.Println("start should be less than end")
		os.Exit(1)
	}

	useNum := strings.Contains(cfg.url, "{NUM}")

	if !useNum {
		g, _ := errgroup.WithContext(context.Background())
		for i := 0; i < cfg.count; i++ {
			g.Go(func() error {
				err := play(cfg.url,
					cfg.transport,
					cfg.url+":"+strconv.Itoa(i),
					cfg.delayTimeout)
				if err != nil {
					log.Println(err)
					os.Exit(1)
				}
				return nil
			})
			<-time.After(cfg.startInterval)
		}
		if err := g.Wait(); err != nil {
			log.Println(err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	g, _ := errgroup.WithContext(context.Background())
	for i := cfg.nStart; i <= cfg.nEnd; i++ {
		u := strings.ReplaceAll(cfg.url, "{NUM}", strconv.Itoa(i))
		g.Go(func() error {
			err := play(u, cfg.transport, u, cfg.delayTimeout)
			if err != nil {
				log.Println(err)
				os.Exit(1)
			}
			return nil
		})
		<-time.After(cfg.startInterval)
	}
	if err := g.Wait(); err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

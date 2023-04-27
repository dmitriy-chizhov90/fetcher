package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

const (
	testDurSecs = 120
	rps = 10000
	maxReqids = 30
)

func main() {
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = 100

	intervalMcs := 1000000 / rps
	intervalStr := fmt.Sprintf("%dus", intervalMcs)
	fmt.Println(intervalStr)
	interval, _ := time.ParseDuration(intervalStr)
	count := testDurSecs * 1000000 / intervalMcs
	fetchCount := count * len(os.Args[1:])
	start := time.Now()
	ch := make(chan fetchResult)
	chResult := make(chan result)
	go aggregate(ch, chResult, fetchCount)
	for i := 0; i < count; i++ {
		for _, url := range os.Args[1:] {
			go fetch(url, ch)
		}
		time.Sleep(interval)
	}

	r := <-chResult

	fmt.Printf("%v\n", r.distr)
	for url, distr := range r.urlDistrs {
		fmt.Printf("%s:\n%v\n", url, distr)
	}
	printReqids("Success reqids:", r.successReqids)
	printReqids("Failed reqids:", r.failReqids)
	fmt.Printf("%.2fs elapsed\n", time.Since(start).Seconds())
}

func printReqids(name string, reqids []string) {
	fmt.Printf("%s :\n", name)
	for _, reqid := range reqids {
		fmt.Printf("https://setrace.dzeninfra.ru/ui/search/?reqid=%s\n", reqid)
	}
}

var latenciesMs = []int64{50, 100, 200, 300}

type fetchResult struct {
	time time.Duration
	url string
	reqid string
}

type urlResult struct {
	distr map[int64]int
}

type result struct {
	urlDistrs map[string]urlResult
	distr map[int64]int
	successReqids []string
	failReqids []string
}

func durationSlot(dur time.Duration) int64 {
	ms := dur.Milliseconds()
	for _, i := range latenciesMs {
		if ms < i {
			return i
		}
	}
	return -1
}

func aggregate(ch <- chan fetchResult, resultCh chan <- result, fetchCount int) {
	urlDistr := make(map[string]urlResult)
	distr := make(map[int64]int)
	var successReqids []string
	var failReqids []string
	var maxDur time.Duration

	perc := 0
	for i := 0; i < fetchCount; i++ {
		fr := <- ch
		if fr.time > maxDur {
			maxDur = fr.time
		}
		slot := durationSlot(fr.time)
		urlRes := urlDistr[fr.url]
		if urlRes.distr == nil {
			urlRes.distr = make(map[int64]int)
		}
		urlRes.distr[slot]++
		urlDistr[fr.url] = urlRes
		distr[slot]++
		if slot < 0 {
			if len(failReqids) < maxReqids {
				failReqids = append(failReqids, fr.reqid)
			}
		} else {
			if len(successReqids) < maxReqids {
				successReqids = append(successReqids, fr.reqid)
			}
		}
		newPerc := i*10/fetchCount
		if newPerc != perc {
			perc = newPerc
			fmt.Printf("%d0%%\n", perc);
		}
	}
	
	v := distr[-1]
	delete(distr, -1)
	distr[maxDur.Milliseconds()] = v

	for _, ud := range urlDistr {
		v := ud.distr[-1]
		delete(ud.distr, -1)
		ud.distr[maxDur.Milliseconds()] = v
	}
	
	resultCh <- result {
		urlDistrs: urlDistr,
		distr: distr,
		successReqids: successReqids,
		failReqids: failReqids,
	}
}

type topsExport struct {
	Reqid string `json:"reqid"`
}

func fetch(url string, ch chan <- fetchResult) {
	client := &http.Client{}
	req, err := http.NewRequest("GET", url, nil)
    if err != nil {
		log.Fatalf("fetch: request was not created: %v\n", err)
	}
	req.Header.Add("User-Agent", "curl/7.68.0")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("fetch: %s, %v\n", url, err)
		ch <- fetchResult { 0, url, "" }
		return
	}
	// Проверить результат выполнения запроса
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("fetch: %s\n", resp.Status)
		log.Printf("%v\n", resp.Header)
		ch <- fetchResult { 0, url, "" }
		return
	}

	duration := time.Since(start)
	byteValue, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("%s: %v\n", url, err)
		ch <- fetchResult { 0, url, "" }
		return
	}
	var tops topsExport
	json.Unmarshal(byteValue, &tops)
	if len(tops.Reqid) == 0 {
		fmt.Printf("reqid is emprt %s\n", url)
		ch <- fetchResult { 0, url, "" }
	}
	ch <- fetchResult {duration, url, tops.Reqid }
}

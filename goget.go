package main

import (
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
	"path"
)

// Structure for Arguments
type Args struct {
	// URL to download
	url string
	// Number of threads to run
	threadcount int
	// Progress Flag - Display Progress
	pflag bool
}

//DownloadTask Single Download Task
type DownloadTask struct {
	url        string
	parts      int
	chunkArray []*FileChunk
	dlsize     int64
	completed  bool
	outputfile *os.File
	totalbdl   int64
	starttime  time.Time
}

//FileChunk Single Chunk of Download
type FileChunk struct {
	job       DownloadTask
	url       string
	chunkdata []byte
	startbyte int64
	endbyte   int64
	resp      *http.Response
	err       error
	buflen    int64
	bytesdone int64
	completed bool
}

/*
This function handles arguments or displays helptext
if arguments are not passed properly.
*/
func processArguments() Args {
	var u = flag.String("u", "", "URL to download")
	var n = flag.Int("n", 5, "Number of download threads (segments)")
	var p = flag.Bool("p", false, "Show progress")

	flag.Parse()
	args := Args{
		url:         *u,
		threadcount: *n,
		pflag:       *p,
	}
	return args
}

func checkMD5(chunk []byte) string {
	hasher := md5.New()
	hasher.Write(chunk)
	return hex.EncodeToString(hasher.Sum(nil))
}

//Initialize the download task
func (dl *DownloadTask) setupDownloadTask(url string, parts int) {
	dl.parts = parts
	dl.url = url
	dl.getURLHead(dl.url)
	dl.launchDLChunks()
	f, err := os.Create(path.Base(dl.url))
	if err != nil {
		dl.outputfile = nil
		return
	}
	dl.outputfile = f
	dl.starttime = time.Now()
	dl.totalbdl = 0
}

//Check if download task is completed
func (dl *DownloadTask) checkComplete() {
	flag := true
	for _, chunk := range dl.chunkArray {
		if chunk.completed == false {
			flag = false
			break
		}
	}
	dl.completed = flag
	time.Sleep(time.Duration(1) * time.Second)
}

// Set up the download chunks
func (dl *DownloadTask) launchDLChunks() {
	v := dl.dlsize
	chunksize := v/int64(dl.parts) + 1
	nextbyte := int64(0)
	for i := v; i > 0; i = i - chunksize {
		//Set up the chunk
		newchunk := new(FileChunk)
		newchunk.setupChunkDownload(dl.url, nextbyte, nextbyte+chunksize)
		nextbyte = nextbyte + chunksize
		dl.chunkArray = append(dl.chunkArray, newchunk)
	}
}

//Initializes the downloads size so that Tasks can be initiated
func (dl *DownloadTask) getURLHead(url string) {
	var netClient = &http.Client{
		//Set a 10s timeout
		Timeout: time.Second * 10,
	}
	resp, err := netClient.Head(url)
	// Handle the error
	if err != nil {
		fmt.Printf("The error code is:\n %s", err)
	}
	defer resp.Body.Close()
	fmt.Printf("The response length is: %d\n", resp.ContentLength)
	if err != nil {
		fmt.Printf("The error code is:\n %s\n", err)
	}
	dl.dlsize = resp.ContentLength
}

//Track global download progress
func (dl *DownloadTask) globalProgress() {
	// Total downloaded btyes
	lastbdl := int64(0)
	for _, chunk := range dl.chunkArray {
		if chunk.completed {
			lastbdl = lastbdl + chunk.endbyte - chunk.startbyte
		} else {
			lastbdl = lastbdl + int64(len(chunk.chunkdata))
		}
	}
	timeelapsed := time.Since(dl.starttime) / time.Second
	speed := ((lastbdl - dl.totalbdl) / 1024) / int64(timeelapsed)
	fmt.Printf("Download Speed: %d KB/s | Downloaded: %d / %d (Total) | Time Elapsed: %d sec \n",
		speed,
		dl.totalbdl,
		dl.dlsize,
		timeelapsed,
	)
	dl.totalbdl = lastbdl
}

func (chunk *FileChunk) setupChunkDownload(url string, startbyte int64, endbyte int64) {
	//initialize the values for the chunk
	chunk.url = url
	chunk.startbyte = startbyte
	chunk.endbyte = endbyte
	chunk.completed = false
	chunk.buflen = 32 * 1024
	chunk.bytesdone = 0
}

func (chunk *FileChunk) downloadChunk() {
	var netClient = &http.Client{}
	// Set the request parameters
	req, _ := http.NewRequest("GET", chunk.url, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", chunk.startbyte, chunk.endbyte-1))
	chunk.resp, chunk.err = netClient.Do(req)
	for {
		buffer := make([]byte, chunk.buflen)
		nr, err := chunk.resp.Body.Read(buffer)
		chunk.chunkdata = append(chunk.chunkdata, buffer[:nr]...)
		chunk.bytesdone = chunk.bytesdone + int64(nr)
		if err == io.EOF {
			chunk.resp.Body.Close()
			break
		}
	}
	chunk.completed = true
}

func (chunk *FileChunk) trackChunkDL() {
	for chunk.completed == false {
		fmt.Printf("Chunk-%d-%d: %d / %d KB done\n",
			chunk.startbyte,
			chunk.endbyte,
			chunk.bytesdone/1024.0,
			(chunk.endbyte-chunk.startbyte+1)/1024.0,
		)
		time.Sleep(time.Duration(1) * time.Second)
	}
}

func (dl *DownloadTask) downloadFile() {
	fmt.Printf("Launching Threads:\n")
	for _, chunk := range dl.chunkArray {
		fmt.Printf("Launching Chunk-%d-%d\n", chunk.startbyte, chunk.endbyte)
		go chunk.downloadChunk()
		//go chunk.trackChunkDL()
	}
	for dl.completed == false {
		time.Sleep(time.Duration(1) * time.Second)
		dl.globalProgress()
		dl.checkComplete()
	}
	defer dl.outputfile.Close()
	for _, chunk := range dl.chunkArray {
		dl.outputfile.Write(chunk.chunkdata)
	}
	timetotal := time.Since(dl.starttime) / time.Second
	fmt.Printf("Download Completed. Average Speed: %d KB/s, Time Eplapsed: %ds \n\n", dl.dlsize/int64(timetotal), timetotal)
}

/* This is the main function */
func main() {
	args := processArguments()
	fmt.Printf("URL:\t\t\t%s\nThreads:\t\t%d\nProgress:\t\t%t\n",
		args.url,
		args.threadcount,
		args.pflag,
	)
	dl := new(DownloadTask)
	dl.setupDownloadTask(args.url, args.threadcount)
	dl.downloadFile()
}
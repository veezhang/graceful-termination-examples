package main

import (
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

type MyHandler struct {
}

func NewMyHandler() *MyHandler {
	return &MyHandler{}
}

func (h *MyHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("job started"))
	go h.slowJob("job1")
	go h.slowJob("job2")
	go h.slowJob("job3")
}

func (h *MyHandler) slowJob(name string) {
	logServer("starting job %q at %s\n", name, time.Now())
	time.Sleep(time.Duration(1+rand.Intn(4)) * time.Second)
	logServer("finished job %q at %s\n", name, time.Now())
}

func mockRequestAndTermination() {
	time.Sleep(1 * time.Second)
	req, err := http.Get("http://127.0.0.1:8080")
	if err != nil {
		panic(err)
	}
	defer func() { req.Body.Close() }()
	msg , _ := io.ReadAll(req.Body)
	logClient("received: %s", msg)

	time.Sleep(2 * time.Second)

	logClient("sending signal %q", strings.Title(syscall.SIGINT.String()))
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("/",  NewMyHandler())
	httpServer := http.Server{
		Addr:              "127.0.0.1:8080",
		Handler:           mux,
	}

	go mockRequestAndTermination()
	if err := httpServer.ListenAndServe(); err != nil {
		logServer("HTTP server shut down")
		os.Exit(1)
	}
}

func logServer(format string, v ...interface{}){
	log.Printf("[S] " + format, v...)
}

func logClient(format string, v ...interface{}){
	log.Printf("[C] " + format, v...)
}

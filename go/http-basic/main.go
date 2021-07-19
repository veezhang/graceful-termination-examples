package main

import (
	"context"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

type MyHandler struct {
	wg *sync.WaitGroup
}

func NewMyHandler(wg *sync.WaitGroup) *MyHandler {
	return &MyHandler{wg: wg}
}

func (h *MyHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Write([]byte("job started"))
	h.wg.Add(3)
	go h.slowJob("job1")
	go h.slowJob("job2")
	go h.slowJob("job3")
}

func (h *MyHandler) slowJob(name string) {
	defer h.wg.Done()
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
	wg := &sync.WaitGroup{}
	mux := http.NewServeMux()
	mux.Handle("/", NewMyHandler(wg))
	httpServer := http.Server{
		Addr:    "127.0.0.1:8080",
		Handler: mux,
	}

	go mockRequestAndTermination()
	go func() {
		logServer("[graceful-termination] http server starting\n")
		if err := httpServer.ListenAndServe(); err != nil {
			if err != http.ErrServerClosed {
				logServer("[graceful-termination] listen failed %s\n", err)
				os.Exit(1)
			}
			logServer("[graceful-termination] http server shutdown\n")
		}
	}()

	termChan := make(chan os.Signal)
	signal.Notify(termChan, syscall.SIGTERM, syscall.SIGINT)

	sig := <-termChan
	logServer("[graceful-termination] received signal %q\n", strings.ToUpper(sig.String()))
	logServer("[graceful-termination] waiting for shutdown to be initiated")

	ctxShutDown, cancelShutDown := context.WithTimeout(context.Background(), 5*time.Second)
	defer func() { cancelShutDown() }()

	if err := httpServer.Shutdown(ctxShutDown); err != nil {
		logServer("[graceful-termination] http server shutdown failed, %s\n", err)
		os.Exit(1)
	}

	logServer("[graceful-termination] waiting jobs to finish\n")
	wg.Wait()
	logServer("[graceful-termination] jobs have finished\n")

	logServer("[graceful-termination] http server is exiting")
}

func logServer(format string, v ...interface{}){
	log.Printf("[S] " + format, v...)
}

func logClient(format string, v ...interface{}){
	log.Printf("[C] " + format, v...)
}

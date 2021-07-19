# Graceful Termination Examples

何为程序的优雅终止？总体来说，就是程序在终止的时候能够做到**不影响业务**，也就是等到相关业务都处理完后再终止服务。

在结束之前常见需要做哪些事情？比如：

* 日志 flush
* 一般网络服务中，停止接受请求，并处理完成相关的业务
* 任务队列模式中，停止任务分发，并处理完成相关的任务
* 服务发现中，向服务注册中心反注册，然后停止接受请求，并处理完成相关的业务
* 待补充

下面将使用不同语言的一些 `examples` 来具体说明下：

## golang

### 一个简单的错误例子

这里是一个简单的错误例子，其中 `mockRequestAndTermination` 是模拟请求然后发送 `SIGINT` 信号来终止程序。

```golang
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
```

执行结果如下：

```shell
$ go run main.go
2021/07/07 13:55:41 [C] received: job started
2021/07/07 13:55:41 [S] starting job "job2" at 2021-07-07 13:55:41.712802 +0800 CST m=+1.003710600
2021/07/07 13:55:41 [S] starting job "job3" at 2021-07-07 13:55:41.712435 +0800 CST m=+1.003344046
2021/07/07 13:55:41 [S] starting job "job1" at 2021-07-07 13:55:41.712493 +0800 CST m=+1.003401942
2021/07/07 13:55:43 [C] sending signal "Interrupt"
2021/07/07 13:55:43 [S] finished job "job2" at 2021-07-07 13:55:43.713896 +0800 CST m=+3.004786641
signal: interrupt
```

由上可以知道 `job1` 和 `job3` 并没有完成，这就不能算优雅的终止了。

### 简单 http 服务

这里使用 `sync.WaitGroup` 来等待所有的请求处理完成后在退出：

```golang
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
```

执行结果如下：

```shell
$ go run main.go
2021/07/07 13:57:45 [S] [graceful-termination] http server starting
2021/07/07 13:57:46 [S] starting job "job2" at 2021-07-07 13:57:46.017109 +0800 CST m=+1.007002711
2021/07/07 13:57:46 [S] starting job "job1" at 2021-07-07 13:57:46.017052 +0800 CST m=+1.006945890
2021/07/07 13:57:46 [S] starting job "job3" at 2021-07-07 13:57:46.017275 +0800 CST m=+1.007169081
2021/07/07 13:57:46 [C] received: job started
2021/07/07 13:57:48 [C] sending signal "Interrupt"
2021/07/07 13:57:48 [S] finished job "job2" at 2021-07-07 13:57:48.021895 +0800 CST m=+3.011770237
2021/07/07 13:57:48 [S] [graceful-termination] received signal "INTERRUPT"
2021/07/07 13:57:48 [S] [graceful-termination] waiting for shutdown to be initiated
2021/07/07 13:57:48 [S] [graceful-termination] http server shutdown
2021/07/07 13:57:48 [S] [graceful-termination] waiting jobs to finish
2021/07/07 13:57:50 [S] finished job "job3" at 2021-07-07 13:57:50.022617 +0800 CST m=+5.012474328
2021/07/07 13:57:50 [S] finished job "job1" at 2021-07-07 13:57:50.022653 +0800 CST m=+5.012511181
2021/07/07 13:57:50 [S] [graceful-termination] jobs have finished
2021/07/07 13:57:50 [S] [graceful-termination] http server is exiting
```

由上可以知道 `job1` 、 `job2` 和 `job3` 都顺利执行完了。

### 等待超时 http 服务

但是有时候，执行的任务非常久，或者程序出现问题导致一直未完成，在这种情况下，我们也不可能一直无限期的等下去。

这时候就设计到需要一个等待超时的时间了。

```golang
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
	h.wg.Add(4)
	go h.slowJob("job1", time.Duration(1+rand.Intn(4)) * time.Second)
	go h.slowJob("job2", time.Duration(1+rand.Intn(4)) * time.Second)
	go h.slowJob("job3", time.Duration(1+rand.Intn(4)) * time.Second)
	go h.slowJob("job4 very slow", time.Hour)
}

func (h *MyHandler) slowJob(name string, dur time.Duration) {
	defer h.wg.Done()
	logServer("starting job %q at %s\n", name, time.Now())
	time.Sleep(dur)
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

	gracePeriod := 30 * time.Second
	ctxJobs, cancelJobs := context.WithTimeout(context.Background(), gracePeriod)
	go func() {
		wg.Wait()
		cancelJobs()
	}()

	logServer("[graceful-termination] waiting jobs to finish\n")
	select {
	case <-ctxJobs.Done():
		logServer("[graceful-termination] jobs have finished\n")
	case <-time.After(gracePeriod):
		logServer("[graceful-termination] wait jobs to finish timeout\n")
	}

	logServer("[graceful-termination] http server is exiting")
}

func logServer(format string, v ...interface{}){
	log.Printf("[S] " + format, v...)
}

func logClient(format string, v ...interface{}){
	log.Printf("[C] " + format, v...)
}
```

执行结果如下：

```shell
$ go run main.go
2021/07/19 11:13:21 [S] [graceful-termination] http server starting
2021/07/19 11:13:22 [S] starting job "job4 very slow" at 2021-07-19 11:13:22.942193 +0800 CST m=+1.004326407
2021/07/19 11:13:22 [S] starting job "job3" at 2021-07-19 11:13:22.942245 +0800 CST m=+1.004378181
2021/07/19 11:13:22 [S] starting job "job1" at 2021-07-19 11:13:22.942062 +0800 CST m=+1.004195255
2021/07/19 11:13:22 [S] starting job "job2" at 2021-07-19 11:13:22.942091 +0800 CST m=+1.004224428
2021/07/19 11:13:22 [C] received: job started
2021/07/19 11:13:24 [S] finished job "job1" at 2021-07-19 11:13:24.943579 +0800 CST m=+3.005792303
2021/07/19 11:13:24 [C] sending signal "Interrupt"
2021/07/19 11:13:24 [S] [graceful-termination] received signal "INTERRUPT"
2021/07/19 11:13:24 [S] [graceful-termination] waiting for shutdown to be initiated
2021/07/19 11:13:24 [S] [graceful-termination] http server shutdown
2021/07/19 11:13:24 [S] [graceful-termination] waiting jobs to finish
2021/07/19 11:13:26 [S] finished job "job2" at 2021-07-19 11:13:26.947428 +0800 CST m=+5.009720829
2021/07/19 11:13:26 [S] finished job "job3" at 2021-07-19 11:13:26.947377 +0800 CST m=+5.009670269
2021/07/19 11:13:54 [S] [graceful-termination] wait jobs to finish timeout
2021/07/19 11:13:54 [S] [graceful-termination] http server is exiting
```

由上可以知道模拟的异常任务 `job4 very slow` 并不会一直等待其完成。

## 待补充其他语言

## Kubernetes

这里有必要强调一下 Kubernetes 中的优雅结束。

待补充

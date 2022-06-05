package routine

import (
	"errors"
	"log"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const DefaultTimeout = 30 * time.Second

type Service struct {
	sigs      chan os.Signal
	mainDone  chan struct{}
	waitGroup sync.WaitGroup
}

func newGraceService() *Service {
	// Go signal notification works by sending `os.Signal`
	// values on a channel. We'll create a channel to
	// receive these notifications (we'll also make one to
	// notify us when the program can exit).

	gs := &Service{
		sigs:     make(chan os.Signal, 1),
		mainDone: make(chan struct{}),
	}
	return gs
}

var gs *Service

// Init grace service
func Init() {
	gs = newGraceService()
	signal.Notify(gs.sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
}

// Wait until all goroutine run by `Go` finished in timeout time.
func Wait() {
	if gs == nil {
		panic("Please call Init() first!")
	}

	go func() {
		gs.waitGroup.Wait()
		close(gs.mainDone)
	}()

	select {
	case sig := <-gs.sigs:
		log.Println("recv signal: ", sig)
		select {
		case <-gs.mainDone:
		}
	case <-gs.mainDone:
	}
}

// Go waiting `function` finished in `timeout` time
// param num in `function` must equal param num in `params`
// param type in `function` must equal param type in `params`
func Go(function interface{}, params ...interface{}) {
	gs.waitGroup.Add(1)
	go gs.safeGo(DefaultTimeout, function, params...)
}

func GoWithTimeout(timeout time.Duration, function interface{}, params ...interface{}) {
	if timeout < time.Duration(0) {
		log.Println("timeout can't less than 0")
	}
	gs.waitGroup.Add(1)
	go gs.safeGo(timeout, function, params...)
}

// 情况1: safego全都正常执行完毕，程序退出
// 情况2: safego未执行完毕时收到kill signal，则safego队列中所有goroutine优雅关闭（等待直至执行完毕），最后退出程序
// 情况3: 某个safego未执行完毕但是timeout，则移出safego的队列，当收到kill signal时直接被kill，不享受优雅关闭
func (gs *Service) safeGo(timeout time.Duration, f interface{}, params ...interface{}) {
	defer func() {
		if err := recover(); err != nil {
			log.Println("SafoGo Recover success, Some error happens:", err)
			printStack()
		}
		// should set wg done anyway
		gs.waitGroup.Done()
	}()

	packed, err := packParams(f, params...)
	if err != nil {
		log.Println(err)
		return
	}

	funcName := runtime.FuncForPC(reflect.ValueOf(f).Pointer()).Name()
	isDone := make(chan struct{})
	normalDoneFlag := false

	//real execution:
	go func() {
		defer func() {
			//函数未正常结束 还是要设置isDone
			if err := recover(); err != nil {
				log.Println("SafeGo recover success, goroutine panic:", err)
				printStack()
				normalDoneFlag = false
			}
			close(isDone)
		}()
		packed()
		normalDoneFlag = true
	}()

	select {
	case <-isDone:
		// the goroutine is done, and will be removed from safego waitgroup
		if !normalDoneFlag {
			log.Println("WARNING:", funcName, "abnormally done")
		}

	case <-time.After(timeout):
		// the goroutine is timeout and will be removed from safeGo waitGroup
		// the timeout goroutine is still running, but can be directly killed by the kill signal.
		// In contrast, the goroutine in the safeGo waitGroup will not be killed by the kill signal
		log.Println("WARNING:", funcName, "timeout, duration: ", timeout)
	}
}

func packParams(function interface{}, params ...interface{}) (func() interface{}, error) {
	fn := reflect.ValueOf(function)
	if fn.Kind() != reflect.Func {
		return nil, errors.New("the first param is not a function")
	}
	inElem := make([]reflect.Value, 0, len(params))
	for _, param := range params {
		inElem = append(inElem, reflect.ValueOf(param))
	}
	if !verifyPackFuncType(fn, inElem) {
		return nil, errors.New("the type of function and params are not matched")
	}

	packedFunc := func() interface{} {
		params := make([]reflect.Value, 0, len(inElem))
		params = append(params, inElem...)
		return fn.Call(params[:])
	}
	return packedFunc, nil
}

func verifyPackFuncType(fn reflect.Value, in []reflect.Value) bool {
	if len(in) != fn.Type().NumIn() {
		return false
	}
	for i := 0; i < len(in); i++ {
		// AssignableTo reports whether a value of the type is assignable to type u.
		// https://golang.org/ref/spec#Assignability
		if !in[i].Type().AssignableTo(fn.Type().In(i)) {
			return false
		}
	}
	return true
}

func printStack() {
	var buf [4096]byte
	n := runtime.Stack(buf[:], false)
	log.Printf("==> %s\n", string(buf[:n]))
}

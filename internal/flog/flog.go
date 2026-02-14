package flog

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Level int

const None Level = -1
const (
	Debug Level = iota
	Info
	Warn
	Error
	Fatal
)

var (
	minLevel  = Info
	logCh     = make(chan string, 1024)
	dropped   atomic.Uint64
	startOnce sync.Once
)

func init() {

}

func SetLevel(l int) {
	minLevel = Level(l)
	if l == int(None) {
		return
	}

	startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case msg, ok := <-logCh:
					if !ok {
						return
					}
					fmt.Fprint(os.Stdout, msg)
				case <-ticker.C:
					if n := dropped.Swap(0); n > 0 {
						now := time.Now().Format("2006-01-02 15:04:05.000")
						fmt.Fprintf(os.Stdout, "%s [WARN] flog: dropped %d log lines (logCh full)\n", now, n)
					}
				}
			}
		}()
	})
}

func logf(level Level, format string, args ...any) {
	if level < minLevel || minLevel == None {
		return
	}

	for i, arg := range args {
		if err, ok := arg.(error); ok {
			if WErr(err) == nil {
				args[i] = "<filtered>"
			}
		}
	}

	now := time.Now().Format("2006-01-02 15:04:05.000")
	line := fmt.Sprintf("%s [%s] %s\n", now, level.String(), fmt.Sprintf(format, args...))

	select {
	case logCh <- line:
	default:
		dropped.Add(1)
	}
}

func (l Level) String() string {
	switch l {
	case Debug:
		return "DEBUG"
	case Info:
		return "INFO"
	case Warn:
		return "WARN"
	case Error:
		return "ERROR"
	case Fatal:
		return "FATAL"
	case None:
		return "None"
	default:
		return "UNKNOWN"
	}
}

func Debugf(format string, args ...any) { logf(Debug, format, args...) }
func Infof(format string, args ...any)  { logf(Info, format, args...) }
func Warnf(format string, args ...any)  { logf(Warn, format, args...) }
func Errorf(format string, args ...any) { logf(Error, format, args...) }
func Fatalf(format string, args ...any) {
	logf(Fatal, format, args...)
	// flush logs (optional: small sleep to let goroutine write)
	time.Sleep(10 * time.Millisecond)
	os.Exit(1)
}

func Close() { close(logCh) }

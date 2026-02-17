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
	minLevel  atomic.Int32
	logCh     = make(chan string, 1024)
	dropped   atomic.Uint64
	startOnce sync.Once
	closeOnce sync.Once
	closed    atomic.Bool
	stopCh    = make(chan struct{})
	doneCh    = make(chan struct{})
)

func init() {
	minLevel.Store(int32(Info))
}

func ensureStarted() {
	startOnce.Do(func() {
		go func() {
			defer close(doneCh)

			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()

			flushDropped := func() {
				if n := dropped.Swap(0); n > 0 {
					now := time.Now().Format("2006-01-02 15:04:05.000")
					fmt.Fprintf(os.Stdout, "%s [WARN] flog: dropped %d log lines (logCh full)\n", now, n)
				}
			}

			for {
				select {
				case msg := <-logCh:
					fmt.Fprint(os.Stdout, msg)
				case <-ticker.C:
					flushDropped()
				case <-stopCh:
					// Drain pending messages before shutdown so Fatal logs are not lost.
					for {
						select {
						case msg := <-logCh:
							fmt.Fprint(os.Stdout, msg)
						default:
							flushDropped()
							return
						}
					}
				}
			}
		}()
	})
}

func SetLevel(l int) {
	minLevel.Store(int32(l))
	if l == int(None) {
		return
	}
	if closed.Load() {
		return
	}
	ensureStarted()
}

func logf(level Level, format string, args ...any) {
	current := Level(minLevel.Load())
	if current == None || level < current {
		return
	}
	ensureStarted()

	for i, arg := range args {
		if err, ok := arg.(error); ok {
			if WErr(err) == nil {
				args[i] = "<filtered>"
			}
		}
	}

	var buf []byte
	buf = time.Now().AppendFormat(buf, "2006-01-02 15:04:05.000")
	buf = append(buf, ' ', '[')
	buf = append(buf, level.String()...)
	buf = append(buf, ']', ' ')
	buf = fmt.Appendf(buf, format, args...)
	buf = append(buf, '\n')
	line := string(buf)

	if closed.Load() {
		fmt.Fprint(os.Stdout, line)
		return
	}

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
	Close()
	os.Exit(1)
}

func Close() {
	closeOnce.Do(func() {
		closed.Store(true)
		ensureStarted()
		close(stopCh)
		<-doneCh
	})
}

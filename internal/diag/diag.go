package diag

import (
	"fmt"
	"paqet/cmd/version"
	"runtime"
	"sync/atomic"
	"time"
)

var startTime = time.Now()

type ConfigInfo struct {
	Role      string `json:"role,omitempty"`
	Interface string `json:"interface,omitempty"`

	IPv4Addr string `json:"ipv4_addr,omitempty"`
	IPv6Addr string `json:"ipv6_addr,omitempty"`

	ServerAddr string `json:"server_addr,omitempty"`
	ListenAddr string `json:"listen_addr,omitempty"`

	Pprof string `json:"pprof,omitempty"`

	Guard bool `json:"guard,omitempty"`
	Conns int  `json:"conns,omitempty"`
}

var cfg atomic.Value // *ConfigInfo

var sessions atomic.Int64
var streams atomic.Int64

var tcpUpBytes atomic.Uint64
var tcpDownBytes atomic.Uint64
var udpUpBytes atomic.Uint64
var udpDownBytes atomic.Uint64

var pingLastAt atomic.Int64  // unix nano
var pingLastRTT atomic.Int64 // nano

var pingLastErr atomic.Value // string

type Status struct {
	Now    time.Time `json:"now"`
	Uptime string    `json:"uptime"`

	Version   string `json:"version"`
	GitTag    string `json:"git_tag"`
	GitCommit string `json:"git_commit"`
	BuildTime string `json:"build_time"`

	Config ConfigInfo `json:"config"`

	Sessions int64 `json:"sessions"`
	Streams  int64 `json:"streams"`

	TCPUpBytes   uint64 `json:"tcp_up_bytes"`
	TCPDownBytes uint64 `json:"tcp_down_bytes"`
	UDPUpBytes   uint64 `json:"udp_up_bytes"`
	UDPDownBytes uint64 `json:"udp_down_bytes"`

	PingLastAt   *time.Time `json:"ping_last_at,omitempty"`
	PingLastRTT  string     `json:"ping_last_rtt,omitempty"`
	PingLastErr  string     `json:"ping_last_err,omitempty"`
	Goroutines   int        `json:"goroutines"`
	AllocBytes   uint64     `json:"alloc_bytes"`
	SysBytes     uint64     `json:"sys_bytes"`
	NumGC        uint32     `json:"num_gc"`
	PauseTotalNs uint64     `json:"pause_total_ns"`
}

func SetConfig(info ConfigInfo) {
	cfg.Store(&info)
}

func IncSessions() { sessions.Add(1) }
func DecSessions() { sessions.Add(-1) }

func IncStreams() { streams.Add(1) }
func DecStreams() { streams.Add(-1) }

func AddTCPUp(n int64) {
	if n > 0 {
		tcpUpBytes.Add(uint64(n))
	}
}
func AddTCPDown(n int64) {
	if n > 0 {
		tcpDownBytes.Add(uint64(n))
	}
}
func AddUDPUp(n int64) {
	if n > 0 {
		udpUpBytes.Add(uint64(n))
	}
}
func AddUDPDown(n int64) {
	if n > 0 {
		udpDownBytes.Add(uint64(n))
	}
}

func SetPing(rtt time.Duration, err error) {
	now := time.Now()
	pingLastAt.Store(now.UnixNano())
	pingLastRTT.Store(int64(rtt))
	if err != nil {
		pingLastErr.Store(err.Error())
	} else {
		pingLastErr.Store("")
	}
}

func Snapshot() Status {
	s := Status{
		Now:          time.Now(),
		Uptime:       time.Since(startTime).Truncate(time.Second).String(),
		Version:      version.Version,
		GitTag:       version.GitTag,
		GitCommit:    version.GitCommit,
		BuildTime:    version.BuildTime,
		Sessions:     sessions.Load(),
		Streams:      streams.Load(),
		TCPUpBytes:   tcpUpBytes.Load(),
		TCPDownBytes: tcpDownBytes.Load(),
		UDPUpBytes:   udpUpBytes.Load(),
		UDPDownBytes: udpDownBytes.Load(),
		Goroutines:   runtime.NumGoroutine(),
	}
	if v := cfg.Load(); v != nil {
		s.Config = *v.(*ConfigInfo)
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	s.AllocBytes = ms.Alloc
	s.SysBytes = ms.Sys
	s.NumGC = ms.NumGC
	s.PauseTotalNs = ms.PauseTotalNs

	if at := pingLastAt.Load(); at > 0 {
		t := time.Unix(0, at)
		s.PingLastAt = &t
	}
	if rtt := pingLastRTT.Load(); rtt > 0 {
		s.PingLastRTT = (time.Duration(rtt)).Truncate(time.Millisecond).String()
	}
	if v := pingLastErr.Load(); v != nil {
		if msg := v.(string); msg != "" {
			s.PingLastErr = msg
		}
	}
	return s
}

func FormatText(s Status) string {
	totalUp := s.TCPUpBytes + s.UDPUpBytes
	totalDown := s.TCPDownBytes + s.UDPDownBytes

	pingLine := "ping: n/a"
	if s.PingLastAt != nil || s.PingLastRTT != "" || s.PingLastErr != "" {
		when := "n/a"
		if s.PingLastAt != nil {
			when = s.PingLastAt.Format(time.RFC3339)
		}
		pingLine = fmt.Sprintf("ping: last_at=%s rtt=%s err=%s", when, s.PingLastRTT, s.PingLastErr)
	}

	return fmt.Sprintf(
		"paqet status\n"+
			"  role: %s\n"+
			"  uptime: %s\n"+
			"  version: %s (tag=%s commit=%s)\n"+
			"  streams: %d  sessions: %d\n"+
			"  bytes: up=%d  down=%d\n"+
			"    tcp: up=%d  down=%d\n"+
			"    udp: up=%d  down=%d\n"+
			"  %s\n"+
			"  runtime: goroutines=%d alloc=%dB sys=%dB gc=%d\n"+
			"  config: iface=%s ipv4=%s ipv6=%s server=%s listen=%s conns=%d guard=%v pprof=%s\n",
		s.Config.Role,
		s.Uptime,
		s.Version, s.GitTag, s.GitCommit,
		s.Streams, s.Sessions,
		totalUp, totalDown,
		s.TCPUpBytes, s.TCPDownBytes,
		s.UDPUpBytes, s.UDPDownBytes,
		pingLine,
		s.Goroutines, s.AllocBytes, s.SysBytes, s.NumGC,
		s.Config.Interface, s.Config.IPv4Addr, s.Config.IPv6Addr, s.Config.ServerAddr, s.Config.ListenAddr, s.Config.Conns, s.Config.Guard, s.Config.Pprof,
	)
}

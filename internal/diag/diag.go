package diag

import (
	"fmt"
	"paqet/cmd/version"
	"runtime"
	"sync/atomic"
	"time"
)

var startTime = time.Now()

// enabled controls whether diag counters are active.
// Keep this false in production unless you need /debug/paqet/* endpoints or counters.
var enabled bool

func Enable(v bool) { enabled = v }
func Enabled() bool { return enabled }

type ConfigInfo struct {
	Role      string `json:"role,omitempty"`
	Interface string `json:"interface,omitempty"`
	DSCP      int    `json:"dscp,omitempty"`

	IPv4Addr string `json:"ipv4_addr,omitempty"`
	IPv6Addr string `json:"ipv6_addr,omitempty"`

	ServerAddr string `json:"server_addr,omitempty"`
	ListenAddr string `json:"listen_addr,omitempty"`

	Pprof string `json:"pprof,omitempty"`

	Guard bool `json:"guard,omitempty"`
	Conns int  `json:"conns,omitempty"`

	// KeyID is a short fingerprint of the shared secret (safe to share).
	// It helps confirm both sides use the same key without revealing it.
	KeyID string `json:"key_id,omitempty"`
}

var cfg atomic.Value // *ConfigInfo

var sessions atomic.Int64
var streams atomic.Int64

var rawUpPackets atomic.Uint64
var rawDownPackets atomic.Uint64
var rawUpBytes atomic.Uint64
var rawDownBytes atomic.Uint64
var rawLastUpAt atomic.Int64   // unix nano
var rawLastDownAt atomic.Int64 // unix nano
var rawUpDrops atomic.Uint64
var rawUpDropBytes atomic.Uint64

// rawDownOversize* tracks frames we captured but dropped because they exceeded the
// receiver buffer size expected by kcp-go (mtuLimit=1500). With GRO/LRO this can
// happen if the kernel coalesces multiple segments and the tunnel can't split them.
var rawDownOversizeDrops atomic.Uint64
var rawDownOversizeDropBytes atomic.Uint64

// rawDownCoalesced* tracks TCP payload coalescing events (GRO/LRO) that we detected
// and split into individual guarded packets.
var rawDownCoalescedFrames atomic.Uint64
var rawDownCoalescedParts atomic.Uint64

var guardPass atomic.Uint64
var guardDrops atomic.Uint64

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

	RawUpPackets   uint64     `json:"raw_up_packets"`
	RawDownPackets uint64     `json:"raw_down_packets"`
	RawUpBytes     uint64     `json:"raw_up_bytes"`
	RawDownBytes   uint64     `json:"raw_down_bytes"`
	RawLastUpAt    *time.Time `json:"raw_last_up_at,omitempty"`
	RawLastDownAt  *time.Time `json:"raw_last_down_at,omitempty"`
	RawUpDrops     uint64     `json:"raw_up_drops"`
	RawUpDropBytes uint64     `json:"raw_up_drop_bytes"`

	RawDownOversizeDrops     uint64 `json:"raw_down_oversize_drops"`
	RawDownOversizeDropBytes uint64 `json:"raw_down_oversize_drop_bytes"`

	RawDownCoalescedFrames uint64 `json:"raw_down_coalesced_frames"`
	RawDownCoalescedParts  uint64 `json:"raw_down_coalesced_parts"`

	GuardPass  uint64 `json:"guard_pass"`
	GuardDrops uint64 `json:"guard_drops"`

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
	if !enabled {
		return
	}
	cfg.Store(&info)
}

func IncSessions() {
	if !enabled {
		return
	}
	sessions.Add(1)
}
func DecSessions() {
	if !enabled {
		return
	}
	sessions.Add(-1)
}

func IncStreams() {
	if !enabled {
		return
	}
	streams.Add(1)
}
func DecStreams() {
	if !enabled {
		return
	}
	streams.Add(-1)
}

func AddRawUp(n int) {
	if !enabled {
		return
	}
	if n > 0 {
		rawUpPackets.Add(1)
		rawUpBytes.Add(uint64(n))
		rawLastUpAt.Store(time.Now().UnixNano())
	}
}

func AddRawUpDrop(n int) {
	if !enabled {
		return
	}
	rawUpDrops.Add(1)
	if n > 0 {
		rawUpDropBytes.Add(uint64(n))
	}
}

func AddRawDown(n int) {
	if !enabled {
		return
	}
	if n > 0 {
		rawDownPackets.Add(1)
		rawDownBytes.Add(uint64(n))
		rawLastDownAt.Store(time.Now().UnixNano())
	}
}

func AddRawDownOversizeDrop(n int) {
	if !enabled {
		return
	}
	rawDownOversizeDrops.Add(1)
	if n > 0 {
		rawDownOversizeDropBytes.Add(uint64(n))
		// Keep RawLastDownAt meaningful even if everything is dropped.
		rawLastDownAt.Store(time.Now().UnixNano())
	}
}

func AddRawDownCoalesced(parts int) {
	if !enabled {
		return
	}
	rawDownCoalescedFrames.Add(1)
	if parts > 0 {
		rawDownCoalescedParts.Add(uint64(parts))
	}
}

func AddGuardPass() {
	if !enabled {
		return
	}
	guardPass.Add(1)
}
func AddGuardDrop() {
	if !enabled {
		return
	}
	guardDrops.Add(1)
}

func AddTCPUp(n int64) {
	if !enabled {
		return
	}
	if n > 0 {
		tcpUpBytes.Add(uint64(n))
	}
}
func AddTCPDown(n int64) {
	if !enabled {
		return
	}
	if n > 0 {
		tcpDownBytes.Add(uint64(n))
	}
}
func AddUDPUp(n int64) {
	if !enabled {
		return
	}
	if n > 0 {
		udpUpBytes.Add(uint64(n))
	}
}
func AddUDPDown(n int64) {
	if !enabled {
		return
	}
	if n > 0 {
		udpDownBytes.Add(uint64(n))
	}
}

func SetPing(rtt time.Duration, err error) {
	if !enabled {
		return
	}
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
		Now:            time.Now(),
		Uptime:         time.Since(startTime).Truncate(time.Second).String(),
		Version:        version.Version,
		GitTag:         version.GitTag,
		GitCommit:      version.GitCommit,
		BuildTime:      version.BuildTime,
		Sessions:       sessions.Load(),
		Streams:        streams.Load(),
		RawUpPackets:   rawUpPackets.Load(),
		RawDownPackets: rawDownPackets.Load(),
		RawUpBytes:     rawUpBytes.Load(),
		RawDownBytes:   rawDownBytes.Load(),
		RawUpDrops:     rawUpDrops.Load(),
		RawUpDropBytes: rawUpDropBytes.Load(),

		RawDownOversizeDrops:     rawDownOversizeDrops.Load(),
		RawDownOversizeDropBytes: rawDownOversizeDropBytes.Load(),
		RawDownCoalescedFrames:   rawDownCoalescedFrames.Load(),
		RawDownCoalescedParts:    rawDownCoalescedParts.Load(),

		GuardPass:    guardPass.Load(),
		GuardDrops:   guardDrops.Load(),
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
	if at := rawLastUpAt.Load(); at > 0 {
		t := time.Unix(0, at)
		s.RawLastUpAt = &t
	}
	if at := rawLastDownAt.Load(); at > 0 {
		t := time.Unix(0, at)
		s.RawLastDownAt = &t
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

	rawLastUp := "n/a"
	if s.RawLastUpAt != nil {
		rawLastUp = s.RawLastUpAt.Format(time.RFC3339)
	}
	rawLastDown := "n/a"
	if s.RawLastDownAt != nil {
		rawLastDown = s.RawLastDownAt.Format(time.RFC3339)
	}

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
			"    raw: packets up=%d  down=%d\n"+
			"    raw: bytes   up=%d  down=%d  last_up=%s  last_down=%s\n"+
			"    raw: drops  packets=%d bytes=%d\n"+
			"    raw: coalesced frames=%d parts=%d\n"+
			"    raw: oversize drops=%d bytes=%d\n"+
			"    guard: pass=%d  drops=%d\n"+
			"    tcp: up=%d  down=%d\n"+
			"    udp: up=%d  down=%d\n"+
			"  %s\n"+
			"  runtime: goroutines=%d alloc=%dB sys=%dB gc=%d\n"+
			"  config: iface=%s dscp=%d ipv4=%s ipv6=%s server=%s listen=%s conns=%d guard=%v key_id=%s pprof=%s\n",
		s.Config.Role,
		s.Uptime,
		s.Version, s.GitTag, s.GitCommit,
		s.Streams, s.Sessions,
		totalUp, totalDown,
		s.RawUpPackets, s.RawDownPackets,
		s.RawUpBytes, s.RawDownBytes, rawLastUp, rawLastDown,
		s.RawUpDrops, s.RawUpDropBytes,
		s.RawDownCoalescedFrames, s.RawDownCoalescedParts,
		s.RawDownOversizeDrops, s.RawDownOversizeDropBytes,
		s.GuardPass, s.GuardDrops,
		s.TCPUpBytes, s.TCPDownBytes,
		s.UDPUpBytes, s.UDPDownBytes,
		pingLine,
		s.Goroutines, s.AllocBytes, s.SysBytes, s.NumGC,
		s.Config.Interface, s.Config.DSCP, s.Config.IPv4Addr, s.Config.IPv6Addr, s.Config.ServerAddr, s.Config.ListenAddr, s.Config.Conns, s.Config.Guard, s.Config.KeyID, s.Config.Pprof,
	)
}

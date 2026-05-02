package metrics

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BotInfo is a static snapshot of a registered bot's metadata.
type BotInfo struct {
	Slug    string `json:"slug"`
	Module  string `json:"module"`
	Repo    string `json:"repo"`
	Enabled bool   `json:"enabled"`
}

// Snapshot is one metrics reading sent to the browser over SSE.
type Snapshot struct {
	UptimeSeconds int64     `json:"uptime_seconds"`
	Goroutines    int       `json:"goroutines"`
	HeapAllocMB   float64   `json:"heap_alloc_mb"`
	HeapInUseMB   float64   `json:"heap_in_use_mb"`
	SysMB         float64   `json:"sys_mb"`
	NumGC         uint32    `json:"num_gc"`
	ProcessRSSMB  float64   `json:"process_rss_mb"`
	CPUPercent    float64   `json:"cpu_percent"`
	SysMemTotalMB float64   `json:"sys_mem_total_mb"`
	SysMemUsedMB  float64   `json:"sys_mem_used_mb"`
	Bots          []BotInfo `json:"bots"`
}

// collector tracks CPU% over time using /proc/self/stat samples.
type collector struct {
	startTime  time.Time
	bots       []BotInfo
	mu         sync.RWMutex
	cpuPercent float64
	lastTicks  uint64
	lastSample time.Time
}

func newCollector(startTime time.Time, bots []BotInfo) *collector {
	c := &collector{
		startTime:  startTime,
		bots:       bots,
		lastSample: time.Now(),
	}
	c.lastTicks, _ = readProcTicks()
	return c
}

func (c *collector) run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.updateCPU()
		}
	}
}

func (c *collector) updateCPU() {
	ticks, err := readProcTicks()
	if err != nil {
		return
	}
	now := time.Now()
	c.mu.Lock()
	elapsed := now.Sub(c.lastSample).Seconds()
	if elapsed > 0 && ticks >= c.lastTicks {
		const clkTck = 100 // typical Linux CONFIG_HZ value
		raw := float64(ticks-c.lastTicks) / clkTck / elapsed * 100
		c.cpuPercent = math.Round(raw*10) / 10
	}
	c.lastTicks = ticks
	c.lastSample = now
	c.mu.Unlock()
}

func (c *collector) snapshot() Snapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	c.mu.RLock()
	cpu := c.cpuPercent
	c.mu.RUnlock()

	totalMB, usedMB := readSysMemory()
	return Snapshot{
		UptimeSeconds: int64(time.Since(c.startTime).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		HeapAllocMB:   round1(float64(ms.HeapAlloc) / 1024 / 1024),
		HeapInUseMB:   round1(float64(ms.HeapInuse) / 1024 / 1024),
		SysMB:         round1(float64(ms.Sys) / 1024 / 1024),
		NumGC:         ms.NumGC,
		ProcessRSSMB:  readProcessRSS(),
		CPUPercent:    cpu,
		SysMemTotalMB: totalMB,
		SysMemUsedMB:  usedMB,
		Bots:          c.bots,
	}
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// readProcTicks reads utime+stime from /proc/self/stat (Linux only).
func readProcTicks() (uint64, error) {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0, err
	}
	// comm field can contain spaces; skip to last ')' to find stable offsets.
	s := string(data)
	idx := strings.LastIndex(s, ")")
	if idx < 0 {
		return 0, fmt.Errorf("parse /proc/self/stat: missing )")
	}
	// After ')': state ppid pgrp session tty tpgid flags minflt cminflt majflt cmajflt utime stime …
	fields := strings.Fields(s[idx+1:])
	if len(fields) < 13 {
		return 0, fmt.Errorf("parse /proc/self/stat: too few fields")
	}
	utime, _ := strconv.ParseUint(fields[11], 10, 64)
	stime, _ := strconv.ParseUint(fields[12], 10, 64)
	return utime + stime, nil
}

// readProcessRSS returns resident set size in MB from /proc/self/status (Linux only).
func readProcessRSS() float64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "VmRSS:") {
			if f := strings.Fields(line); len(f) >= 2 {
				kb, _ := strconv.ParseFloat(f[1], 64)
				return round1(kb / 1024)
			}
		}
	}
	return 0
}

// readSysMemory returns total and used system RAM in MB from /proc/meminfo (Linux only).
func readSysMemory() (totalMB, usedMB float64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var totalKB, availKB uint64
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		val, _ := strconv.ParseUint(f[1], 10, 64)
		switch f[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availKB = val
		}
	}
	var usedKB uint64
	if totalKB > availKB {
		usedKB = totalKB - availKB
	}
	return round1(float64(totalKB) / 1024), round1(float64(usedKB) / 1024)
}

// Server is a standalone HTTP metrics server on a dedicated port.
type Server struct {
	srv    *http.Server
	col    *collector
	logger *slog.Logger
}

// New creates a metrics server. Call Start to begin serving.
func New(addr string, startTime time.Time, bots []BotInfo, logger *slog.Logger) *Server {
	col := newCollector(startTime, bots)
	s := &Server{col: col, logger: logger}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.page)
	mux.HandleFunc("GET /stream", s.stream)

	s.srv = &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return s
}

// Start launches the CPU sampler and HTTP listener in background goroutines.
func (s *Server) Start(ctx context.Context) {
	go s.col.run(ctx)
	go func() {
		s.logger.Info("metrics server listening", "addr", s.srv.Addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("metrics server stopped", "error", err)
		}
	}()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.srv.Shutdown(ctx)
}

func (s *Server) page(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(metricsHTML))
}

func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() {
		data, err := json.Marshal(s.col.snapshot())
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", data)
		_ = rc.Flush()
	}

	send()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			send()
		}
	}
}

const metricsHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>bots-platform · metrics</title>
<style>
*{box-sizing:border-box;margin:0;padding:0}
body{font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;background:#f0f0f1;color:#1d2327;min-height:100vh}
.topbar{background:#1d2327;padding:16px 28px;display:flex;align-items:center;justify-content:space-between}
.logo{font-size:18px;font-weight:700;color:#f0f0f1;letter-spacing:.5px}
.logo span{color:#72aee6}
.live{display:inline-flex;align-items:center;gap:6px;font-size:12px;color:#a7aaad}
.dot{width:8px;height:8px;border-radius:50%;background:#00a32a;animation:blink 1.6s infinite}
.dot.err{background:#d63638;animation:none}
@keyframes blink{0%,100%{opacity:1}50%{opacity:.3}}
.content{padding:28px;max-width:1100px}
h2{font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.9px;color:#787c82;margin:28px 0 12px}
h2:first-child{margin-top:0}
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:14px}
.card{background:#fff;border:1px solid #c3c4c7;border-radius:4px;padding:16px 18px;box-shadow:0 1px 1px rgba(0,0,0,.04)}
.card .lbl{font-size:11px;color:#787c82;text-transform:uppercase;letter-spacing:.8px;margin-bottom:8px}
.card .val{font-size:28px;font-weight:700;color:#1d2327;line-height:1}
.card .unit{font-size:11px;color:#787c82;margin-top:5px}
.bar-wrap{margin-top:10px;background:#f0f0f1;border-radius:2px;height:4px}
.bar{height:4px;border-radius:2px;background:#2271b1;transition:width .6s,background .4s}
.bar.warn{background:#dba617}.bar.crit{background:#d63638}
.charts{display:grid;grid-template-columns:1fr 1fr;gap:14px}
.chart-card{background:#fff;border:1px solid #c3c4c7;border-radius:4px;padding:16px 18px;box-shadow:0 1px 1px rgba(0,0,0,.04)}
.chart-card .lbl{font-size:11px;color:#787c82;text-transform:uppercase;letter-spacing:.8px;margin-bottom:12px;display:flex;justify-content:space-between}
.chart-card canvas{display:block;width:100%;height:100px}
.table-wrap{background:#fff;border:1px solid #c3c4c7;border-radius:4px;overflow:hidden;box-shadow:0 1px 1px rgba(0,0,0,.04)}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;padding:10px 14px;font-size:11px;text-transform:uppercase;letter-spacing:.7px;color:#787c82;border-bottom:1px solid #c3c4c7;background:#f9f9f9}
td{padding:10px 14px;border-bottom:1px solid #f0f0f1;color:#3c434a;vertical-align:middle}
tr:last-child td{border-bottom:none}
tr:hover td{background:#f9f9f9}
.badge{display:inline-block;padding:2px 9px;border-radius:9999px;font-size:11px;font-weight:600}
.badge-green{background:#edfaef;color:#00a32a;border:1px solid #b8e6be}
.badge-gray{background:#f0f0f1;color:#50575e;border:1px solid #c3c4c7}
code{background:#f0f0f1;padding:1px 5px;border-radius:2px;font-size:12px}
@media(max-width:700px){.stats{grid-template-columns:1fr 1fr}.charts{grid-template-columns:1fr}}
</style>
</head>
<body>
<div class="topbar">
  <div class="logo">bots&#8209;<span>platform</span> &middot; metrics</div>
  <span class="live"><span class="dot" id="dot"></span>real&#8209;time</span>
</div>
<div class="content">

<h2>Обзор</h2>
<div class="stats">
  <div class="card">
    <div class="lbl">Аптайм</div>
    <div class="val" id="uptime" style="font-size:20px">—</div>
  </div>
  <div class="card">
    <div class="lbl">CPU бота</div>
    <div class="val" id="cpu">—</div>
    <div class="unit">% от одного ядра</div>
    <div class="bar-wrap"><div class="bar" id="cpu-bar" style="width:0%"></div></div>
  </div>
  <div class="card">
    <div class="lbl">RAM бота</div>
    <div class="val" id="rss">—</div>
    <div class="unit">МБ — реальное потребление</div>
  </div>
  <div class="card">
    <div class="lbl">RAM сервера</div>
    <div class="val" id="sys-used">—</div>
    <div class="unit" id="sys-total-lbl">МБ / — МБ всего</div>
    <div class="bar-wrap"><div class="bar" id="sys-bar" style="width:0%"></div></div>
  </div>
</div>

<h2>График за последние 2 минуты</h2>
<div class="charts">
  <div class="chart-card">
    <div class="lbl"><span>CPU бота</span><span id="cpu-cur">—</span></div>
    <canvas id="cpu-chart" height="100"></canvas>
  </div>
  <div class="chart-card">
    <div class="lbl"><span>RAM сервера</span><span id="ram-cur">—</span></div>
    <canvas id="ram-chart" height="100"></canvas>
  </div>
</div>

<h2>Боты на платформе</h2>
<div class="table-wrap">
  <table>
    <thead>
      <tr><th>Slug</th><th>Module</th><th>Репозиторий</th><th>Статус</th></tr>
    </thead>
    <tbody id="bots-tbody">
      <tr><td colspan="4" style="color:#787c82;text-align:center;padding:20px">загрузка…</td></tr>
    </tbody>
  </table>
</div>

</div>
<script>
const HISTORY=60;
const cpuH=[],ramH=[];

function fmt(n,dec){return Number(n).toFixed(dec===undefined?1:dec)}
function uptime(s){
  const d=Math.floor(s/86400),h=Math.floor(s%86400/3600),m=Math.floor(s%3600/60),sec=s%60;
  if(d>0)return d+'д '+h+'ч '+m+'м';
  if(h>0)return h+'ч '+m+'м '+sec+'с';
  if(m>0)return m+'м '+sec+'с';
  return sec+'с';
}
function set(id,v){const e=document.getElementById(id);if(e)e.textContent=v}
function bar(id,pct){
  const e=document.getElementById(id);if(!e)return;
  const p=Math.min(Math.max(pct,0),100);
  e.style.width=p+'%';
  e.className='bar'+(p>=90?' crit':p>=70?' warn':'');
}
function esc(s){return String(s).replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;')}

function drawChart(canvasId,data,maxVal,color,unit){
  const canvas=document.getElementById(canvasId);
  if(!canvas)return;
  const dpr=window.devicePixelRatio||1;
  const W=canvas.offsetWidth,H=100;
  canvas.width=W*dpr;canvas.height=H*dpr;
  const ctx=canvas.getContext('2d');
  ctx.scale(dpr,dpr);
  const top=8,bot=H-4,left=0,right=W;
  const rangeH=bot-top;
  const mx=maxVal>0?maxVal:1;

  // grid
  ctx.strokeStyle='#f0f0f1';ctx.lineWidth=1;
  [0.25,0.5,0.75,1].forEach(f=>{
    const y=bot-rangeH*f;
    ctx.beginPath();ctx.moveTo(left,y);ctx.lineTo(right,y);ctx.stroke();
  });

  if(data.length<2)return;
  const step=(right-left)/(HISTORY-1);

  // fill
  const grad=ctx.createLinearGradient(0,top,0,bot);
  grad.addColorStop(0,color+'33');grad.addColorStop(1,color+'00');
  ctx.fillStyle=grad;
  ctx.beginPath();
  data.forEach((v,i)=>{
    const x=left+(HISTORY-data.length+i)*step;
    const y=bot-rangeH*(Math.min(v,mx)/mx);
    i===0?ctx.moveTo(x,y):ctx.lineTo(x,y);
  });
  ctx.lineTo(left+(HISTORY-1)*step,bot);
  ctx.lineTo(left+(HISTORY-data.length)*step,bot);
  ctx.closePath();ctx.fill();

  // line
  ctx.strokeStyle=color;ctx.lineWidth=2;ctx.lineJoin='round';
  ctx.beginPath();
  data.forEach((v,i)=>{
    const x=left+(HISTORY-data.length+i)*step;
    const y=bot-rangeH*(Math.min(v,mx)/mx);
    i===0?ctx.moveTo(x,y):ctx.lineTo(x,y);
  });
  ctx.stroke();
}

function push(arr,val){arr.push(val);if(arr.length>HISTORY)arr.shift()}

const src=new EventSource('/stream');
src.onmessage=function(e){
  const d=JSON.parse(e.data);
  set('uptime',uptime(d.uptime_seconds));

  set('cpu',fmt(d.cpu_percent));
  bar('cpu-bar',d.cpu_percent);
  set('rss',fmt(d.process_rss_mb,0));
  set('sys-used',fmt(d.sys_mem_used_mb,0));
  set('sys-total-lbl','МБ / '+fmt(d.sys_mem_total_mb,0)+' МБ всего');
  if(d.sys_mem_total_mb>0)bar('sys-bar',d.sys_mem_used_mb/d.sys_mem_total_mb*100);

  push(cpuH,d.cpu_percent);
  push(ramH,d.sys_mem_used_mb);

  set('cpu-cur',fmt(d.cpu_percent)+'%');
  set('ram-cur',fmt(d.sys_mem_used_mb,0)+' / '+fmt(d.sys_mem_total_mb,0)+' МБ');

  drawChart('cpu-chart',cpuH,100,'#2271b1','%');
  drawChart('ram-chart',ramH,d.sys_mem_total_mb,'#00a32a','МБ');

  const tb=document.getElementById('bots-tbody');
  if(tb&&d.bots){
    tb.innerHTML=d.bots.length?d.bots.map(b=>
      '<tr>'+
      '<td><code>'+esc(b.slug)+'</code></td>'+
      '<td>'+esc(b.module)+'</td>'+
      '<td style="color:#787c82;font-size:12px">'+esc(b.repo)+'</td>'+
      '<td>'+(b.enabled?'<span class="badge badge-green">активен</span>':'<span class="badge badge-gray">отключён</span>')+'</td>'+
      '</tr>').join(''):'<tr><td colspan="4" style="color:#787c82;text-align:center">нет ботов</td></tr>';
  }
};
src.onerror=function(){document.getElementById('dot').className='dot err'};
window.addEventListener('resize',()=>{
  drawChart('cpu-chart',cpuH,100,'#2271b1','%');
  if(ramH.length){const last=ramH[ramH.length-1];drawChart('ram-chart',ramH,last*1.5,'#00a32a','МБ')}
});
</script>
</body>
</html>`

// qualify-external is a bounded local-only protocol qualification bridge.
// Credentials remain inside the existing Hermes container; no files or services
// are installed on the VPS. Never use it as a public deployment proxy.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/publicapi"
)

const remotePython = `import os,sys,json,urllib.request,urllib.error
body=sys.stdin.buffer.read(8388609)
if len(body)>8388608:raise SystemExit('request too large')
b=os.environ.get('OPENAI_BASE_URL','').rstrip('/')
k=os.environ.get('OPENAI_API_KEY','')
if b!='http://cli-proxy-api:8317/v1' or not k:raise SystemExit('unexpected private route')
request=urllib.request.Request(b+'/responses',data=body,headers={'Authorization':'Bearer '+k,'Content-Type':'application/json'},method='POST')
try:
 response=urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request,timeout=60)
except urllib.error.HTTPError as e:
 print(json.dumps({'status':e.code,'content_type':'application/json'}),flush=True)
 print(json.dumps({'error':'Private qualification upstream rejected the request','status':e.code}),flush=True)
 raise SystemExit(0)
except Exception:
 print(json.dumps({'status':502,'content_type':'application/json'}),flush=True)
 print(json.dumps({'error':'Private qualification connection failed'}),flush=True)
 raise SystemExit(0)
print(json.dumps({'status':response.status,'content_type':response.headers.get('Content-Type','application/json')}),flush=True)
with response:
 for line in response:
  sys.stdout.buffer.write(line);sys.stdout.buffer.flush()
`

func main() {
	allow := flag.Bool("allow-live", false, "explicitly enable bounded Antigravity calls through the private SSH route")
	listen := flag.String("listen", "127.0.0.1:18517", "IPv4 loopback listener")
	maximum := flag.Int64("max-requests", 6, "maximum model requests before fail-closed")
	flag.Parse()
	if !*allow || *maximum < 1 || *maximum > 6 {
		log.Fatal("requires --allow-live; max-requests must be 1..6")
	}
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "127.0.0.1" {
		log.Fatal("IPv4 loopback is required")
	}
	var calls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer qualification-fixture" {
			http.Error(w, "qualification credential required", 401)
			return
		}
		if calls.Add(1) > *maximum {
			http.Error(w, "qualification request cap reached", 429)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 75*time.Second)
		defer cancel()
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, publicapi.BodyLimit))
		if err != nil {
			http.Error(w, "invalid body", 413)
			return
		}
		session := publicapi.NewSession()
		body, err = session.Normalize(body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		remote := "docker exec -i hermes python -c '" + strings.ReplaceAll(remotePython, "'", "'\"'\"'") + "'"
		cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "netcup-hermes", remote)
		cmd.Stdin = bytes.NewReader(body)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			http.Error(w, "SSH setup failed", 502)
			return
		}
		// Do not forward SSH/docker stderr or request data to the client/log.
		cmd.Stderr = io.Discard
		if err = cmd.Start(); err != nil {
			http.Error(w, "SSH start failed", 502)
			return
		}
		reader := bufio.NewReader(stdout)
		line, err := reader.ReadBytes('\n')
		var header struct {
			Status      int    `json:"status"`
			ContentType string `json:"content_type"`
		}
		if err != nil || json.Unmarshal(line, &header) != nil || header.Status < 100 {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			http.Error(w, "private bridge failed", 502)
			return
		}
		w.Header().Set("Content-Type", header.ContentType)
		w.WriteHeader(header.Status)
		if !strings.Contains(header.ContentType, "text/event-stream") {
			data, readErr := io.ReadAll(io.LimitReader(reader, 8<<20))
			if readErr == nil {
				_, _ = io.WriteString(w, session.RestoreJSON(string(data)))
			}
		} else {
			var event strings.Builder
			for {
				line, readErr := reader.ReadString('\n')
				event.WriteString(line)
				if strings.TrimSpace(line) == "" && event.Len() > 0 {
					for _, restored := range session.RestoreSSE([]string{event.String()}) {
						_, _ = io.WriteString(w, restored)
					}
					event.Reset()
					if flush, ok := w.(http.Flusher); ok {
						flush.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}
		}
		_ = cmd.Wait()
		log.Printf("qualification request %d completed, HTTP %d", calls.Load(), header.Status)
	})
	fmt.Printf("Private qualification bridge listening on %s; cap %d model requests\n", *listen, *maximum)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 80 * time.Second}
	log.Fatal(server.ListenAndServe())
}

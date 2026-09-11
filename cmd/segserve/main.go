//go:build segserve

/*
 * Serve an ffmpeg HLS directory the way LANcast's API serves one, and log what
 * the engine asks for.
 *
 * The file HLS path fails in LANcast's window and plays in the same WebView2
 * runtime when a plain static server hands it the same bytes. So the fault is
 * in delivery, and this reproduces delivery: the playlist rewritten to session
 * URLs, segments through http.ServeContent with the same headers, optionally
 * over TLS with a self-signed certificate the window is pinned to. Every
 * request is logged with its Range header, status and bytes — the view of the
 * element's behaviour nothing else here gives.
 *
 *   go build -tags segserve -o segserve.exe ./cmd/segserve
 *   ./segserve.exe -dir <ffmpeg output dir> -addr 127.0.0.1:8110 [-tls]
 *
 * Behind a build tag: a diagnostic that ships is a diagnostic somebody runs.
 */
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const prefix = "/api/stream/7058/hls/sess0001/"

// rewrite mirrors internal/api rewritePlaylist exactly.
func rewrite(body, prefix string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			continue
		case strings.HasPrefix(t, "#EXT-X-MAP:URI="):
			lines[i] = `#EXT-X-MAP:URI="` + prefix + "init.mp4\""
		case strings.HasPrefix(t, "#"):
			continue
		default:
			lines[i] = prefix + t
		}
	}
	return strings.Join(lines, "\n")
}

// syntheticPlaylist lists every segment a full encode will produce.
func syntheticPlaylist(total, seg float64, prefix string) string {
	var b strings.Builder
	n := int(total / seg)
	if float64(n)*seg < total {
		n++
	}
	nl := string(rune(10))
	fmt.Fprintf(&b, "#EXTM3U"+nl+"#EXT-X-VERSION:7"+nl+"#EXT-X-TARGETDURATION:%d"+nl+"#EXT-X-MEDIA-SEQUENCE:0"+nl, int(seg+0.999))
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD" + nl + "#EXT-X-INDEPENDENT-SEGMENTS" + nl)
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=%q"+nl, prefix+"init.mp4")
	for i := 0; i < n; i++ {
		d := seg
		if rem := total - float64(i)*seg; rem < seg {
			d = rem
		}
		fmt.Fprintf(&b, "#EXTINF:%.6f,"+nl+"%sseg%05d.m4s"+nl, d, prefix, i)
	}
	b.WriteString("#EXT-X-ENDLIST" + nl)
	return b.String()
}

func waitFor(path string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func waitListed(dir, name string, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if b, err := os.ReadFile(filepath.Join(dir, "index.m3u8")); err == nil {
			if name == "init.mp4" || strings.Contains(string(b), name) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

type rec struct {
	http.ResponseWriter
	status int
	n      int
}

func (r *rec) WriteHeader(c int) { r.status = c; r.ResponseWriter.WriteHeader(c) }
func (r *rec) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	n, err := r.ResponseWriter.Write(p)
	r.n += n
	return n, err
}

const page = `<!doctype html><meta charset="utf-8"><title>segserve</title>
<video id="v" controls muted autoplay playsinline src="/api/stream/7058/hls/index.m3u8" style="width:640px"></video>
<pre id="log"></pre>
<script>
const v = document.getElementById("v"), out = [], t0 = performance.now();
function say(s){ const m = ((performance.now()-t0)/1000).toFixed(2)+"s "+s; out.push(m); document.getElementById("log").textContent = out.join("\n"); }
for (const ev of ["loadstart","loadedmetadata","loadeddata","canplay","playing","waiting","stalled","seeking","seeked","ended","emptied","abort"]) {
  v.addEventListener(ev, () => say(ev+" ready="+v.readyState+" net="+v.networkState+" t="+v.currentTime.toFixed(2)+" buf="+(v.buffered.length? v.buffered.end(v.buffered.length-1).toFixed(2):"none")));
}
v.addEventListener("error", () => say("ERROR code="+(v.error&&v.error.code)+" message="+JSON.stringify(v.error&&v.error.message)+" ready="+v.readyState+" net="+v.networkState));
say("ua "+navigator.userAgent);
const who = new URLSearchParams(location.search).get("who") || "x";
setTimeout(() => { say("END t="+v.currentTime.toFixed(2)+" ready="+v.readyState+" paused="+v.paused); fetch("/report/"+who, {method:"POST", body: out.join(" | ")}); }, 40000);
</script>`

func main() {
	dir := flag.String("dir", "", "ffmpeg HLS output directory")
	addr := flag.String("addr", "127.0.0.1:8110", "listen address")
	useTLS := flag.Bool("tls", false, "serve over TLS with a self-signed certificate")
	reports := flag.String("reports", ".", "where page reports are written")
	synthetic := flag.Float64("synthetic", 0, "serve a complete VOD playlist for this many seconds of media instead of ffmpeg's")
	segSeconds := flag.Float64("segseconds", 6, "segment length the synthetic playlist lists")
	flag.Parse()
	if *dir == "" {
		log.Fatal("need -dir")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/test.html", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, page)
	})
	mux.HandleFunc("/report/", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		name := filepath.Join(*reports, "report-"+strings.TrimPrefix(r.URL.Path, "/report/")+".txt")
		_ = os.WriteFile(name, b, 0o644)
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/stream/7058/hls/index.m3u8", func(w http.ResponseWriter, r *http.Request) {
		if *synthetic > 0 {
			// The whole film, listed up front and marked finished, so the engine
			// has no reason to reload it. Segments not yet written are waited
			// for by the segment route below, exactly as the API does.
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, syntheticPlaylist(*synthetic, *segSeconds, prefix))
			return
		}
		p := filepath.Join(*dir, "index.m3u8")
		if !waitFor(p, 30*time.Second) {
			http.Error(w, `{"error":"unavailable"}`, 503)
			return
		}
		body, err := os.ReadFile(p)
		if err != nil {
			http.Error(w, "read", 500)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(rewrite(string(body), prefix)))
	})
	mux.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, prefix)
		if filepath.Base(name) != name {
			http.Error(w, "bad", 400)
			return
		}
		p := filepath.Join(*dir, name)
		ready := waitFor(p, 30*time.Second)
		if *synthetic > 0 {
			// The engine asks ahead of the encode, and ffmpeg writes segments in
			// place: existing is not finished. A segment is finished once ffmpeg's
			// own playlist lists it; init once that playlist exists at all.
			ready = waitListed(*dir, name, 60*time.Second)
		}
		if !ready {
			http.Error(w, "unavailable", 503)
			return
		}
		f, err := os.Open(p)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		if strings.HasSuffix(name, ".mp4") {
			w.Header().Set("Content-Type", "video/mp4")
		} else {
			w.Header().Set("Content-Type", "video/iso.segment")
		}
		w.Header().Set("Cache-Control", "no-store")
		info, _ := f.Stat()
		http.ServeContent(w, r, name, info.ModTime(), f)
	})

	logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &rec{ResponseWriter: w}
		mux.ServeHTTP(rw, r)
		fmt.Printf("%s %s %s range=%q -> %d %d bytes %dms\n", time.Now().Format("15:04:05.000"),
			r.Method, r.URL.Path, r.Header.Get("Range"), rw.status, rw.n, time.Since(start).Milliseconds())
	})

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{Handler: logged}
	if !*useTLS {
		fmt.Println("serving http on", *addr)
		log.Fatal(srv.Serve(ln))
	}
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "LANcast segserve"},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	cert, _ := x509.ParseCertificate(der)
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	fmt.Println("serving https on", *addr, "pin", base64.StdEncoding.EncodeToString(sum[:]))
	srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	log.Fatal(srv.ServeTLS(ln, "", ""))
}

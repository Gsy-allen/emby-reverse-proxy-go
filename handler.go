package main

import (
	"compress/gzip"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// copyBufPool reuses 32 KB buffers for io.CopyBuffer.
// 32 KB balances syscall count vs memory: 1 MB = 32 syscalls, not 256.
// nginx proxy_buffering off only disables disk buffering, not socket buffers.
var copyBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 32*1024)
		return &buf
	},
}

// gzipWriterPool reuses gzip writers to avoid heavy init cost per response.
var gzipWriterPool = sync.Pool{
	New: func() any {
		w, _ := gzip.NewWriterLevel(nil, gzip.BestSpeed)
		return w
	},
}

type ProxyHandler struct {
	client         *http.Client
	resolver       *net.Resolver
	allowUnsafeDNS bool
}

const upstreamDialTimeout = 30 * time.Second
const upstreamKeepAlive = 60 * time.Second

type resolvedTargetContextKey struct{}

func parseBlockPrivateTargets(raw string) bool {
	if raw == "" {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func NewProxyHandler(blockPrivateTargets bool) *ProxyHandler {
	h := &ProxyHandler{resolver: net.DefaultResolver, allowUnsafeDNS: !blockPrivateTargets}
	transport := &http.Transport{
		MaxIdleConns:          200,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       50,
		IdleConnTimeout:       120 * time.Second,
		TLSClientConfig:       &tls.Config{},
		DialContext:           h.dialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 300 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// Must be true for a proxy — prevents Go from silently
		// decompressing upstream gzip in memory then sending uncompressed.
		DisableCompression: true,
		ForceAttemptHTTP2:  true,
		// Restore sensible buffer sizes. nginx proxy_buffering off only skips
		// disk buffering; kernel socket buffers remain large.
		// 32 KB keeps syscall count low without excess pre-read.
		WriteBufferSize: 32 * 1024,
		ReadBufferSize:  32 * 1024,
	}

	h.client = &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return h
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t, err := parseTarget(r.URL.Path, r.URL.RawQuery)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var rt *resolvedTarget
	if !h.allowUnsafeDNS {
		rt, err = resolveSafeTarget(r.Context(), h.resolver, t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), resolvedTargetContextKey{}, rt))
	}

	start := time.Now()
	if isWebSocketRequest(r) {
		h.serveWebSocket(w, r, t, rt, start)
		return
	}

	h.serveHTTPProxy(w, r, t, start)
}

func resolvedTargetFromContext(ctx context.Context) (*resolvedTarget, error) {
	rt, ok := ctx.Value(resolvedTargetContextKey{}).(*resolvedTarget)
	if !ok || rt == nil {
		return nil, fmt.Errorf("missing resolved target")
	}
	if len(rt.dialAddrs) == 0 {
		return nil, fmt.Errorf("missing resolved target addresses")
	}
	return rt, nil
}

func dialResolvedAddresses(ctx context.Context, network string, dialer *net.Dialer, addrs []string, onConn func(net.Conn) (net.Conn, error)) (net.Conn, error) {
	if len(addrs) == 0 {
		return nil, fmt.Errorf("missing resolved target addresses")
	}
	var lastErr error
	for _, addr := range addrs {
		conn, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			lastErr = err
			continue
		}
		if onConn == nil {
			return conn, nil
		}
		wrapped, err := onConn(conn)
		if err == nil {
			return wrapped, nil
		}
		conn.Close()
		lastErr = err
	}
	if lastErr == nil {
		return nil, fmt.Errorf("missing resolved target addresses")
	}
	return nil, lastErr
}

func (h *ProxyHandler) dialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: upstreamDialTimeout, KeepAlive: upstreamKeepAlive}
	if h.allowUnsafeDNS {
		return dialer.DialContext(ctx, network, addr)
	}
	rt, err := resolvedTargetFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return dialResolvedAddresses(ctx, network, dialer, rt.dialAddresses(), nil)
}

func (h *ProxyHandler) serveHTTPProxy(w http.ResponseWriter, r *http.Request, t *target, start time.Time) {
	baseURL := inferBaseURL(r)
	media := looksLikeMedia(t.Path)

	upstreamTarget := *t
	upstreamTarget.Path = rewriteBarePlaybackPathForHTTP(t.Path)
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, buildTargetURL(&upstreamTarget), r.Body)
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	copyRequestHeaders(outReq.Header, r.Header, false)
	if rng := r.Header.Get("Range"); rng != "" {
		outReq.Header.Set("Range", rng)
	}
	if ifr := r.Header.Get("If-Range"); ifr != "" {
		outReq.Header.Set("If-Range", ifr)
	}
	setUpstreamHost(outReq, t)
	rewriteProxySensitiveRequestHeaders(outReq.Header, r.Header.Get("X-Forwarded-Prefix"))
	if shouldRewriteEmbyPath(&upstreamTarget) {
		outReq.Header.Set("Accept-Encoding", "identity")
	}

	resp, err := h.client.Do(outReq)
	if err != nil {
		log.Printf("[ERROR] %s %s/%s upstream request failed: %v", r.Method, t.Domain, t.Path, err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	rewriteResponseHeaders(resp, t, baseURL)
	copyResponseHeaders(w.Header(), resp.Header)

	ct := resp.Header.Get("Content-Type")
	contentEncoding := normalizeContentEncoding(resp.Header.Get("Content-Encoding"))
	if shouldRewriteEmbyResponse(t, ct) && safeContentEncodings[contentEncoding] && responseAllowsBody(r.Method, resp.StatusCode) {
		h.serveRewrittenBody(w, r, resp, t, baseURL)
		log.Printf("[API] %d %s %s/%s | rewritten | %s", resp.StatusCode, r.Method, t.Domain, t.Path, time.Since(start))
		return
	}

	written := h.serveStreamBody(w, resp, t)
	elapsed := time.Since(start)
	if media {
		log.Printf("[STREAM] %d %s %s/%s | bytes %s | %s",
			resp.StatusCode, r.Method, t.Domain, t.Path,
			formatBytes(written), elapsed)
		return
	}
	log.Printf("[PROXY] %d %s %s/%s | bytes %s | %s",
		resp.StatusCode, r.Method, t.Domain, t.Path,
		formatBytes(written), elapsed)
}

func (h *ProxyHandler) serveRewrittenBody(w http.ResponseWriter, r *http.Request, resp *http.Response, t *target, baseURL string) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[ERROR] %s %s/%s read rewritten body failed: %v", r.Method, t.Domain, t.Path, err)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	rewritten := rewriteBody(body, baseURL)

	// Compress rewritten text responses if client supports gzip.
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Del("Content-Length")
		w.WriteHeader(resp.StatusCode)

		gz := gzipWriterPool.Get().(*gzip.Writer)
		gz.Reset(w)
		_, writeErr := gz.Write(rewritten)
		closeErr := gz.Close()
		gzipWriterPool.Put(gz)
		if writeErr != nil {
			logExpectedDisconnect(writeErr, "%s %s/%s write gzipped response failed", r.Method, t.Domain, t.Path)
		}
		if closeErr != nil {
			logExpectedDisconnect(closeErr, "%s %s/%s close gzip writer failed", r.Method, t.Domain, t.Path)
		}
	} else {
		w.Header().Set("Content-Length", strconv.Itoa(len(rewritten)))
		w.Header().Del("Content-Encoding")
		w.WriteHeader(resp.StatusCode)
		if _, err := w.Write(rewritten); err != nil {
			logExpectedDisconnect(err, "%s %s/%s write rewritten response failed", r.Method, t.Domain, t.Path)
		}
	}
}

// serveStreamBody pipes upstream to client via io.CopyBuffer with pooled buffer.
// No intermediate buffering — on Linux this can trigger splice(2).
func (h *ProxyHandler) serveStreamBody(w http.ResponseWriter, resp *http.Response, t *target) int64 {
	w.WriteHeader(resp.StatusCode)
	bufp := copyBufPool.Get().(*[]byte)
	written, err := io.CopyBuffer(w, resp.Body, *bufp)
	copyBufPool.Put(bufp)
	if err != nil {
		logExpectedDisconnect(err, "%s/%s stream copy failed", t.Domain, t.Path)
	}
	return written
}

func responseAllowsBody(method string, statusCode int) bool {
	if method == http.MethodHead {
		return false
	}
	if statusCode >= 100 && statusCode < 200 {
		return false
	}
	return statusCode != http.StatusNoContent && statusCode != http.StatusNotModified
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

var mediaExtensions = map[string]bool{
	"mp4": true, "mkv": true, "avi": true, "ts": true, "m3u8": true,
	"mp3": true, "flac": true, "aac": true, "ogg": true, "wav": true,
	"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "ico": true,
	"bmp": true, "tiff": true, "svg": true,
	"woff": true, "woff2": true, "ttf": true, "eot": true,
	"zip": true, "gz": true, "br": true, "zst": true,
	"m4v": true, "m4a": true, "webm": true, "mov": true, "wmv": true,
	"srt": true, "ass": true, "ssa": true, "vtt": true, "sub": true,
}

func looksLikeMedia(path string) bool {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "/videos/") ||
		strings.Contains(lower, "/audio/") ||
		strings.Contains(lower, "/images/") ||
		strings.Contains(lower, "/items/images") ||
		strings.Contains(lower, "/stream") {
		return true
	}
	dot := strings.LastIndexByte(path, '.')
	if dot >= 0 && dot < len(path)-1 {
		ext := strings.ToLower(path[dot+1:])
		if q := strings.IndexByte(ext, '?'); q >= 0 {
			ext = ext[:q]
		}
		return mediaExtensions[ext]
	}
	return false
}

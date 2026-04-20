package main

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newUnsafeTestProxyHandler() *ProxyHandler {
	h := NewProxyHandler(true)
	h.allowUnsafeDNS = true
	return h
}

func newProxyRequest(method, path string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	return req
}

func serveProxyRequest(t *testing.T, upstream http.HandlerFunc, requestPath string) (*httptest.ResponseRecorder, int) {
	t.Helper()
	server := httptest.NewServer(upstream)
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, newProxyRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+requestPath))
	return rr, port
}

func assertResponseStatus(t *testing.T, rr *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rr.Code != want {
		t.Fatalf("status = %d, want %d", rr.Code, want)
	}
}

func assertBodyContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("body missing %q in %q", want, body)
	}
}

func TestLooksLikeMedia(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/Videos/123/stream.mp4", want: true},
		{path: "/Items/Images/Primary", want: true},
		{path: "/audio/track.flac", want: true},
		{path: "/web/index.html", want: false},
		{path: "/emby/Items/1", want: false},
	}

	for _, tt := range tests {
		if got := looksLikeMedia(tt.path); got != tt.want {
			t.Fatalf("looksLikeMedia(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestShouldRewriteEmbyPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/Items/123/PlaybackInfo", want: true},
		{path: "/emby/Items/123/PlaybackInfo", want: true},
		{path: "/emby/Sessions/Playing/Progress", want: true},
		{path: "/Items", want: false},
		{path: "/emby/Items/1", want: false},
		{path: "/web/index.html", want: false},
		{path: "/Videos/123/stream.mp4", want: false},
	}

	for _, tt := range tests {
		if got := shouldRewriteEmbyPath(&target{Path: strings.TrimPrefix(tt.path, "/")}); got != tt.want {
			t.Fatalf("shouldRewriteEmbyPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestNormalizeContentEncoding(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "gzip", want: "gzip"},
		{raw: "GZIP, br", want: "gzip"},
		{raw: " identity ", want: "identity"},
		{raw: "", want: ""},
	}

	for _, tt := range tests {
		if got := normalizeContentEncoding(tt.raw); got != tt.want {
			t.Fatalf("normalizeContentEncoding(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestServeHTTPBareItemsCountsRewritesToEmbyPrefix(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/emby/Items/Counts" {
			t.Fatalf("path = %q, want %q", got, "/emby/Items/Counts")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MovieCount":1}`))
	}, "/Items/Counts")

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBareServerDomainsRewritesToEmbyPrefix(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/emby/System/Ext/ServerDomains" {
			t.Fatalf("path = %q, want %q", got, "/emby/System/Ext/ServerDomains")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}, "/System/Ext/ServerDomains")

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBarePlaybackInfoRewritesToEmbyPrefix(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.URL.Path; got != "/emby/Items/123/PlaybackInfo" {
			t.Fatalf("path = %q, want %q", got, "/emby/Items/123/PlaybackInfo")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"MediaSources":[]}`))
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodPost, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Items/123/PlaybackInfo", strings.NewReader(`{}`))
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBareAdditionalPartsRewritesToEmbyPrefix(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/emby/Videos/123/AdditionalParts" {
			t.Fatalf("path = %q, want %q", got, "/emby/Videos/123/AdditionalParts")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}, "/Videos/123/AdditionalParts")

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBareSimilarRewritesToEmbyPrefix(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/emby/Items/123/Similar" {
			t.Fatalf("path = %q, want %q", got, "/emby/Items/123/Similar")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Items":[]}`))
	}, "/Items/123/Similar")

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBareMediaPathStaysUnchanged(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/Videos/123/original.mkv" {
			t.Fatalf("path = %q, want %q", got, "/Videos/123/original.mkv")
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("ok"))
	}, "/Videos/123/original.mkv")

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPBadTarget(t *testing.T) {
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestServeHTTPRewriteBodyPath(t *testing.T) {
	var port int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "" {
			t.Fatalf("Accept-Encoding = %q, want empty", got)
		}
		if got := r.Header.Get("Referer"); got != "https://upstream.example.com/app" {
			t.Fatalf("Referer = %q, want https://upstream.example.com/app", got)
		}
		if got := r.Header.Get("Origin"); got != "https://upstream.example.com/app" {
			t.Fatalf("Origin = %q, want https://upstream.example.com/app", got)
		}
		if got := r.Header.Get("X-Forwarded-For"); got != "" {
			t.Fatalf("X-Forwarded-For = %q, want empty", got)
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"url":"http://127.0.0.1:` + strconv.Itoa(port) + `/Items/1"}`))
	}))
	defer upstream.Close()

	port = upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Items", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/custom-prefix")
	req.Header.Set("Referer", "https://proxy.example.com/custom-prefix/https/upstream.example.com/443/app")
	req.Header.Set("Origin", "https://proxy.example.com/custom-prefix/https/upstream.example.com/443/app")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	want := `{"url":"http://127.0.0.1:` + strconv.Itoa(port) + `/Items/1"}`
	if got := strings.TrimSpace(rr.Body.String()); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestServeHTTPMediaRequestPreservesClientAcceptEncoding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "gzip, br" {
			t.Fatalf("Accept-Encoding = %q, want gzip, br", got)
		}
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Videos/123/stream.mp4", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Accept-Encoding", "gzip, br")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPNonRewritePathPreservesClientAcceptEncoding(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept-Encoding"); got != "gzip, br" {
			t.Fatalf("Accept-Encoding = %q, want gzip, br", got)
		}
		w.Header().Set("Content-Type", "text/css")
		_, _ = w.Write([]byte("body{color:red}"))
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/custom/site.css", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("Accept-Encoding", "gzip, br")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusOK)
}

func TestServeHTTPRewriteRedirectHeaders(t *testing.T) {
	var port int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1:"+strconv.Itoa(port)+"/web/index.html")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	port = upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/redirect", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	want := "https://proxy.example.com/http/127.0.0.1/" + strconv.Itoa(port) + "/web/index.html"
	if got := rr.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestServeHTTPRewriteRedirectHeadersWithForwardedPrefix(t *testing.T) {
	var port int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://127.0.0.1:"+strconv.Itoa(port)+"/web/index.html")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	port = upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/redirect", nil)
	req.Host = "proxy.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Prefix", "/custom-prefix")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusFound)
	}
	want := "https://proxy.example.com/custom-prefix/http/127.0.0.1/" + strconv.Itoa(port) + "/web/index.html"
	if got := rr.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestServeHTTPRedirectRewritesThirdPartyStreamLocation(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://cdn.example.com/redirect")
		w.WriteHeader(http.StatusFound)
	}, "/redirect")

	assertResponseStatus(t, rr, http.StatusFound)
	want := "https://proxy.example.com/https/cdn.example.com/443/redirect"
	if got := rr.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestServeHTTPRedirectNonAbsoluteLocationRewritesToProxyPath(t *testing.T) {
	rr, port := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/img/poster/123/img.webp@400_")
		w.WriteHeader(http.StatusFound)
	}, "/emby/Items/123/Images/Primary")

	assertResponseStatus(t, rr, http.StatusFound)
	want := "https://proxy.example.com/http/127.0.0.1/" + strconv.Itoa(port) + "/img/poster/123/img.webp@400_"
	if got := rr.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestServeHTTPImageRedirectRegressionWithForwardedPrefixAndQuery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/emby/Items/m01KMFN6SX7XMD3ZGZFRYWS5W9W/Images/Primary" {
			t.Fatalf("path = %q, want %q", got, "/emby/Items/m01KMFN6SX7XMD3ZGZFRYWS5W9W/Images/Primary")
		}
		if got := r.URL.RawQuery; got != "maxWidth=400&tag=p01KMFN6SX7XMD3ZGZFRYWS5W9W&quality=90" {
			t.Fatalf("query = %q, want %q", got, "maxWidth=400&tag=p01KMFN6SX7XMD3ZGZFRYWS5W9W&quality=90")
		}
		w.Header().Set("Location", "/img/poster/01KMFN6SX7XMD3ZGZFRYWS5W9W/img.webp@400_?v=1")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := newProxyRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/emby/Items/m01KMFN6SX7XMD3ZGZFRYWS5W9W/Images/Primary?maxWidth=400&tag=p01KMFN6SX7XMD3ZGZFRYWS5W9W&quality=90")
	req.Header.Set("X-Forwarded-Prefix", "/emby-proxy")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusMovedPermanently)
	want := "https://proxy.example.com/emby-proxy/http/127.0.0.1/" + strconv.Itoa(port) + "/img/poster/01KMFN6SX7XMD3ZGZFRYWS5W9W/img.webp@400_?v=1"
	if got := rr.Header().Get("Location"); got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestServeHTTPContentLocationRelativePathRewritesToProxyPath(t *testing.T) {
	rr, port := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Location", "/img/backdrop/123/original.webp")
		w.Header().Set("Content-Type", "image/webp")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("image-bytes"))
	}, "/emby/Items/123/Images/Backdrop")

	assertResponseStatus(t, rr, http.StatusOK)
	want := "https://proxy.example.com/http/127.0.0.1/" + strconv.Itoa(port) + "/img/backdrop/123/original.webp"
	if got := rr.Header().Get("Content-Location"); got != want {
		t.Fatalf("Content-Location = %q, want %q", got, want)
	}
}

func TestServeHTTPStreamPathAndRange(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Range"); got != "bytes=0-15" {
			t.Fatalf("Range = %q, want bytes=0-15", got)
		}
		if got := r.Header.Get("If-Range"); got != "etag-1" {
			t.Fatalf("If-Range = %q, want etag-1", got)
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Videos/1/stream.mp4", nil)
	req.Header.Set("Range", "bytes=0-15")
	req.Header.Set("If-Range", "etag-1")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusPartialContent)
	}
	if !bytes.Equal(rr.Body.Bytes(), payload) {
		t.Fatal("stream body mismatch")
	}
}

func TestServeHTTPCompressedResponseFallsBackToStream(t *testing.T) {
	payload := []byte("gzip-body")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Items", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if !bytes.Equal(rr.Body.Bytes(), payload) {
		t.Fatalf("body = %q, want %q", rr.Body.Bytes(), payload)
	}
}

func TestServeHTTPRemovesSensitiveResponseHeaders(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "upstream")
		w.Header().Set("X-Powered-By", "go")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	port := upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/blob.bin", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	for _, name := range []string{"Server", "X-Powered-By"} {
		if got := rr.Header().Get(name); got != "" {
			t.Fatalf("%s = %q, want empty", name, got)
		}
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestServeHTTPBlocksDangerousTarget(t *testing.T) {
	handler := NewProxyHandler(true)
	req := httptest.NewRequest(http.MethodGet, "/http/127.0.0.1/8096/Items", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
	if !strings.Contains(rr.Body.String(), "blocked target host") {
		t.Fatalf("body = %q, want blocked target host message", rr.Body.String())
	}
}

func TestResolvedTargetFromContextRequiresAddresses(t *testing.T) {
	_, err := resolvedTargetFromContext(context.WithValue(context.Background(), resolvedTargetContextKey{}, &resolvedTarget{}))
	if err == nil || !strings.Contains(err.Error(), "missing resolved target addresses") {
		t.Fatalf("resolvedTargetFromContext() error = %v, want missing resolved target addresses", err)
	}
}

func TestDialContextUsesResolvedTargetAddress(t *testing.T) {
	handler := NewProxyHandler(true)
	handler.allowUnsafeDNS = false

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	accepted := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			accepted <- ""
			return
		}
		accepted <- conn.RemoteAddr().String()
		conn.Close()
	}()

	target := &target{Scheme: "http", Domain: "example.com", Port: ln.Addr().(*net.TCPAddr).Port}
	rt := &resolvedTarget{dialAddrs: []string{net.JoinHostPort("127.0.0.1", strconv.Itoa(target.Port))}}
	ctx := context.WithValue(context.Background(), resolvedTargetContextKey{}, rt)

	conn, err := handler.dialContext(ctx, "tcp", net.JoinHostPort(target.Domain, strconv.Itoa(target.Port)))
	if err != nil {
		t.Fatalf("dialContext() error = %v", err)
	}
	conn.Close()

	if got := <-accepted; got == "" {
		t.Fatal("expected listener to accept resolved target connection")
	}
}

func TestDialContextFallsBackToNextResolvedTargetAddress(t *testing.T) {
	handler := NewProxyHandler(true)
	handler.allowUnsafeDNS = false

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()

	accepted := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			conn.Close()
			accepted <- struct{}{}
		}
	}()

	resolved := &resolvedTarget{
		dialAddrs: []string{
			net.JoinHostPort("127.0.0.2", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)),
			net.JoinHostPort("127.0.0.1", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)),
		},
	}
	ctx := context.WithValue(context.Background(), resolvedTargetContextKey{}, resolved)

	conn, err := handler.dialContext(ctx, "tcp", net.JoinHostPort("example.com", strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)))
	if err != nil {
		t.Fatalf("dialContext() fallback error = %v", err)
	}
	conn.Close()

	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected listener to accept fallback resolved target connection")
	}
}

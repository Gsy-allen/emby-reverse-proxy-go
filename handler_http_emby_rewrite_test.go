package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestServeHTTPRewriteBodyPathRewritesStreamHost(t *testing.T) {
	var port int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"emby":"http://127.0.0.1:` + strconv.Itoa(port) + `/Items/1","stream":"https://stream.example.com/video.mp4"}`))
	}))
	defer upstream.Close()

	port = upstream.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, newProxyRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Items"))

	assertResponseStatus(t, rr, http.StatusOK)
	want := `{"emby":"https://proxy.example.com/http/127.0.0.1/` + strconv.Itoa(port) + `/Items/1","stream":"https://proxy.example.com/https/stream.example.com/443/video.mp4"}`
	if got := strings.TrimSpace(rr.Body.String()); got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestServeHTTPRewriteBodyEmbyMediaSourcesRegression(t *testing.T) {
	rr, _ := serveProxyRequest(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{
			"Items": [{
				"Id": "123",
				"MediaSources": [{
					"Id": "ms1",
					"Path": "https://stream-cdn.example.com/videos/123/master.m3u8?MediaSourceId=ms1&api_key=test-key",
					"TranscodingUrl": "http://transcode-node.example.com:8096/Videos/123/master.m3u8?DeviceId=device-1&MediaSourceId=ms1"
				}]
			}]
		}`))
	}, "/Items")

	assertResponseStatus(t, rr, http.StatusOK)
	body := rr.Body.String()
	assertBodyContains(t, body, `https://proxy.example.com/https/stream-cdn.example.com/443/videos/123/master.m3u8?MediaSourceId=ms1&api_key=test-key`)
	assertBodyContains(t, body, `https://proxy.example.com/http/transcode-node.example.com/8096/Videos/123/master.m3u8?DeviceId=device-1&MediaSourceId=ms1`)
}

func TestServeHTTPRewriteBodyEmbyMediaSourcesWithForwardedPrefix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{
			"Items": [{
				"Id": "123",
				"MediaSources": [{
					"Id": "ms1",
					"Path": "https://stream-cdn.example.com/videos/123/master.m3u8?MediaSourceId=ms1&api_key=test-key",
					"TranscodingUrl": "http://transcode-node.example.com:8096/Videos/123/master.m3u8?DeviceId=device-1&MediaSourceId=ms1"
				}]
			}]
		}`))
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	req := newProxyRequest(http.MethodGet, "/http/127.0.0.1/"+strconv.Itoa(port)+"/Items")
	req.Header.Set("X-Forwarded-Prefix", "/custom-prefix")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusOK)
	body := rr.Body.String()
	assertBodyContains(t, body, `https://proxy.example.com/custom-prefix/https/stream-cdn.example.com/443/videos/123/master.m3u8?MediaSourceId=ms1&api_key=test-key`)
	assertBodyContains(t, body, `https://proxy.example.com/custom-prefix/http/transcode-node.example.com/8096/Videos/123/master.m3u8?DeviceId=device-1&MediaSourceId=ms1`)
}

func TestServeHTTPRewriteBodySkipsNoContentProgressResponsePost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	rr := httptest.NewRecorder()
	req := newProxyRequest(http.MethodPost, "/http/127.0.0.1/"+strconv.Itoa(port)+"/emby/Sessions/Playing/Progress")
	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusNoContent)
	if body := rr.Body.String(); body != "" {
		t.Fatalf("body = %q, want empty", body)
	}
	if got := rr.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want empty", got)
	}
}

func TestServeHTTPRewriteBodyStillRewritesNormalPostJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Path":"https://stream-cdn.example.com/videos/123/master.m3u8?MediaSourceId=ms1"}`))
	}))
	defer server.Close()

	port := server.Listener.Addr().(*net.TCPAddr).Port
	handler := newUnsafeTestProxyHandler()
	rr := httptest.NewRecorder()
	req := newProxyRequest(http.MethodPost, "/http/127.0.0.1/"+strconv.Itoa(port)+"/emby/Sessions/Playing/Progress")
	handler.ServeHTTP(rr, req)

	assertResponseStatus(t, rr, http.StatusOK)
	assertBodyContains(t, rr.Body.String(), `https://proxy.example.com/https/stream-cdn.example.com/443/videos/123/master.m3u8?MediaSourceId=ms1`)
}

func TestResponseAllowsBody(t *testing.T) {
	tests := []struct {
		name   string
		method string
		status int
		want   bool
	}{
		{name: "get ok", method: http.MethodGet, status: http.StatusOK, want: true},
		{name: "head ok", method: http.MethodHead, status: http.StatusOK, want: false},
		{name: "no content", method: http.MethodPost, status: http.StatusNoContent, want: false},
		{name: "not modified", method: http.MethodGet, status: http.StatusNotModified, want: false},
		{name: "informational", method: http.MethodGet, status: http.StatusContinue, want: false},
	}

	for _, tt := range tests {
		if got := responseAllowsBody(tt.method, tt.status); got != tt.want {
			t.Fatalf("responseAllowsBody(%q, %d) = %v, want %v", tt.method, tt.status, got, tt.want)
		}
	}
}

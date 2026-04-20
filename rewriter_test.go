package main

import "testing"

func TestShouldRewriteBody(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		want        bool
	}{
		{name: "json with charset", contentType: "application/json; charset=utf-8", want: true},
		{name: "html", contentType: "text/html", want: true},
		{name: "xml", contentType: "application/xml", want: true},
		{name: "image png", contentType: "image/png", want: false},
		{name: "gzip archive", contentType: "application/gzip", want: false},
		{name: "invalid keeps false", contentType: "@@@", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRewriteBody(tt.contentType); got != tt.want {
				t.Fatalf("shouldRewriteBody(%q) = %v, want %v", tt.contentType, got, tt.want)
			}
		})
	}
}

func TestShouldRewriteEmbyResponse(t *testing.T) {
	tests := []struct {
		name        string
		target      *target
		contentType string
		want        bool
	}{
		{name: "playback info json", target: &target{Path: "Items/123/PlaybackInfo"}, contentType: "application/json", want: true},
		{name: "sessions progress json", target: &target{Path: "emby/Sessions/Playing/Progress"}, contentType: "application/json", want: true},
		{name: "items list json", target: &target{Path: "Items"}, contentType: "application/json", want: false},
		{name: "web html", target: &target{Path: "web/index.html"}, contentType: "text/html", want: false},
		{name: "plain root html", target: &target{Path: ""}, contentType: "text/html", want: false},
		{name: "non-emby plain text", target: &target{Path: "notes/readme.txt"}, contentType: "text/plain", want: false},
		{name: "binary media path", target: &target{Path: "Videos/1/stream.mp4"}, contentType: "video/mp4", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRewriteEmbyResponse(tt.target, tt.contentType); got != tt.want {
				t.Fatalf("shouldRewriteEmbyResponse(%q, %q) = %v, want %v", tt.target.Path, tt.contentType, got, tt.want)
			}
		})
	}
}

func TestRewriteSingleURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		baseURL string
		want    string
	}{
		{
			name:    "rewrite https default port",
			rawURL:  "https://emby.example.com/web/index.html?x=1",
			baseURL: "https://proxy.example.com",
			want:    "https://proxy.example.com/https/emby.example.com/443/web/index.html?x=1",
		},
		{
			name:    "rewrite third-party stream url too",
			rawURL:  "https://cdn.example.com/video.mp4",
			baseURL: "https://proxy.example.com",
			want:    "https://proxy.example.com/https/cdn.example.com/443/video.mp4",
		},
		{
			name:    "leave relative URL untouched",
			rawURL:  "/web/index.html",
			baseURL: "https://proxy.example.com",
			want:    "/web/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rewriteSingleURL(tt.rawURL, tt.baseURL); got != tt.want {
				t.Fatalf("rewriteSingleURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteBody(t *testing.T) {
	body := []byte(`{"api":"https://emby.example.com/Items/1","fallback":"http://cdn.example.com:8096/video.mp4","relative":"/Items/2"}`)
	baseURL := "https://proxy.example.com"
	want := `{"api":"https://proxy.example.com/https/emby.example.com/443/Items/1","fallback":"https://proxy.example.com/http/cdn.example.com/8096/video.mp4","relative":"/Items/2"}`

	if got := string(rewriteBody(body, baseURL)); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLWithPrefixedBaseURL(t *testing.T) {
	got := rewriteSingleURL("https://emby.example.com/web/index.html?x=1", "https://proxy.example.com/custom-prefix")
	want := "https://proxy.example.com/custom-prefix/https/emby.example.com/443/web/index.html?x=1"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithPrefixedBaseURL(t *testing.T) {
	body := []byte(`{"main":"https://emby.example.com/Items/1","stream":"https://stream.example.com/Videos/1/stream.mp4"}`)
	want := `{"main":"https://proxy.example.com/custom-prefix/https/emby.example.com/443/Items/1","stream":"https://proxy.example.com/custom-prefix/https/stream.example.com/443/Videos/1/stream.mp4"}`
	if got := string(rewriteBody(body, "https://proxy.example.com/custom-prefix")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLDefaultPortMatch(t *testing.T) {
	got := rewriteSingleURL("http://emby.example.com/system/info", "https://proxy.example.com")
	want := "https://proxy.example.com/http/emby.example.com/80/system/info"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithoutAbsoluteURL(t *testing.T) {
	body := []byte(`{"relative":"/Items/2"}`)
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != string(body) {
		t.Fatalf("rewriteBody() = %q, want %q", got, string(body))
	}
}

func TestRewriteBodyKeepsQueryString(t *testing.T) {
	body := []byte(`{"url":"https://stream.example.com/Videos/1/stream.mp4?api_key=abc&Static=true"}`)
	want := `{"url":"https://proxy.example.com/https/stream.example.com/443/Videos/1/stream.mp4?api_key=abc&Static=true"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLInvalidLeavesOriginal(t *testing.T) {
	got := rewriteSingleURL("javascript:void(0)", "https://proxy.example.com")
	if got != "javascript:void(0)" {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, "javascript:void(0)")
	}
}

func TestShouldRewriteEmbyResponseNonEmbyPath(t *testing.T) {
	target := &target{Path: "docs/readme.txt"}
	if shouldRewriteEmbyResponse(target, "text/plain") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for non-Emby path")
	}
}

func TestShouldRewriteEmbyResponseEmbyPath(t *testing.T) {
	target := &target{Path: "emby/Items/123/PlaybackInfo"}
	if !shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = false, want true for playback info path")
	}
}

func TestShouldRewriteEmbyResponseWebPath(t *testing.T) {
	target := &target{Path: "web/index.html"}
	if shouldRewriteEmbyResponse(target, "text/html") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for web path")
	}
}

func TestShouldRewriteEmbyResponseRootPath(t *testing.T) {
	target := &target{Path: ""}
	if shouldRewriteEmbyResponse(target, "text/html") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for root path")
	}
}

func TestShouldRewriteEmbyResponseUnsupportedType(t *testing.T) {
	target := &target{Path: "emby/Items"}
	if shouldRewriteEmbyResponse(target, "image/png") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for image/png")
	}
}

func TestRewriteBodyRewritesMixedHosts(t *testing.T) {
	body := []byte(`{"main":"https://emby.example.com/Items/1","stream":"https://stream.example.com/Videos/1/stream.mp4"}`)
	want := `{"main":"https://proxy.example.com/https/emby.example.com/443/Items/1","stream":"https://proxy.example.com/https/stream.example.com/443/Videos/1/stream.mp4"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLHTTPCustomPort(t *testing.T) {
	got := rewriteSingleURL("http://stream.example.com:8096/Videos/1/stream.mp4", "https://proxy.example.com")
	want := "https://proxy.example.com/http/stream.example.com/8096/Videos/1/stream.mp4"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyPlainTextStreamURL(t *testing.T) {
	body := []byte("stream=https://stream.example.com/Videos/1/stream.mp4")
	want := "stream=https://proxy.example.com/https/stream.example.com/443/Videos/1/stream.mp4"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLHandlesMissingPath(t *testing.T) {
	got := rewriteSingleURL("https://stream.example.com", "https://proxy.example.com")
	want := "https://proxy.example.com/https/stream.example.com/443/"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyLeavesMalformedURLAsProxyEncodedBestEffort(t *testing.T) {
	body := []byte(`{"url":"https://example.com"}`)
	want := `{"url":"https://proxy.example.com/https/example.com/443/"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLHandlesIPv6(t *testing.T) {
	got := rewriteSingleURL("http://[2001:db8::1]:8096/web/index.html", "https://proxy.example.com")
	want := "https://proxy.example.com/http/2001:db8::1/8096/web/index.html"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyMultipleURLs(t *testing.T) {
	body := []byte(`one=https://a.example.com/x two=http://b.example.com:8080/y`)
	want := `one=https://proxy.example.com/https/a.example.com/443/x two=https://proxy.example.com/http/b.example.com/8080/y`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyNoHTTPFastPath(t *testing.T) {
	body := []byte("plain text only")
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != string(body) {
		t.Fatalf("rewriteBody() = %q, want %q", got, string(body))
	}
}

func TestRewriteSingleURLPreservesFragmentLikeSuffixInPath(t *testing.T) {
	got := rewriteSingleURL("https://example.com/web/index.html#frag", "https://proxy.example.com")
	want := "https://proxy.example.com/https/example.com/443/web/index.html#frag"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestShouldRewriteEmbyResponseCaseInsensitivePath(t *testing.T) {
	target := &target{Path: "EMBY/Items/123/PlaybackInfo"}
	if !shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = false, want true for case-insensitive playback info path")
	}
}

func TestShouldRewriteEmbyResponsePlaylistPath(t *testing.T) {
	target := &target{Path: "Playlists/123/Items"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for playlist path")
	}
}

func TestShouldRewriteEmbyResponsePlainTextOnRoot(t *testing.T) {
	target := &target{Path: ""}
	if shouldRewriteEmbyResponse(target, "text/plain") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for root text response")
	}
}

func TestRewriteBodyWithPortlessHTTPURL(t *testing.T) {
	body := []byte(`{"url":"http://example.com/test"}`)
	want := `{"url":"https://proxy.example.com/http/example.com/80/test"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithPortlessHTTPSURL(t *testing.T) {
	body := []byte(`{"url":"https://example.com/test"}`)
	want := `{"url":"https://proxy.example.com/https/example.com/443/test"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyRespectsURLTerminators(t *testing.T) {
	body := []byte(`{"url":"https://example.com/test", "next":1}`)
	want := `{"url":"https://proxy.example.com/https/example.com/443/test", "next":1}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithParenthesesTerminator(t *testing.T) {
	body := []byte(`(https://example.com/test)`)
	want := `(https://proxy.example.com/https/example.com/443/test)`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithBracketsTerminator(t *testing.T) {
	body := []byte(`[https://example.com/test]`)
	want := `[https://proxy.example.com/https/example.com/443/test]`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithXMLLikeTerminator(t *testing.T) {
	body := []byte(`<loc>https://example.com/test</loc>`)
	want := `<loc>https://proxy.example.com/https/example.com/443/test</loc>`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLKeepsRelativeUntouched(t *testing.T) {
	got := rewriteSingleURL("/Items/1", "https://proxy.example.com")
	if got != "/Items/1" {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, "/Items/1")
	}
}

func TestRewriteBodyWithBackToBackURLs(t *testing.T) {
	body := []byte(`https://a.example.com/x https://b.example.com/y`)
	want := `https://proxy.example.com/https/a.example.com/443/x https://proxy.example.com/https/b.example.com/443/y`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyEmpty(t *testing.T) {
	if got := string(rewriteBody([]byte{}, "https://proxy.example.com")); got != "" {
		t.Fatalf("rewriteBody() = %q, want empty", got)
	}
}

func TestRewriteSingleURLEmpty(t *testing.T) {
	if got := rewriteSingleURL("", "https://proxy.example.com"); got != "" {
		t.Fatalf("rewriteSingleURL() = %q, want empty", got)
	}
}

func TestRewriteBodyOnlyHTTPSubstring(t *testing.T) {
	body := []byte(`{"value":"http-not-url"}`)
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != string(body) {
		t.Fatalf("rewriteBody() = %q, want %q", got, string(body))
	}
}

func TestRewriteBodyHTTPSPreferredWhenOverlap(t *testing.T) {
	body := []byte(`https://example.com/test`)
	want := `https://proxy.example.com/https/example.com/443/test`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyHTTPURL(t *testing.T) {
	body := []byte(`http://example.com/test`)
	want := `https://proxy.example.com/http/example.com/80/test`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyPreservesLeadingText(t *testing.T) {
	body := []byte(`prefix https://example.com/test`)
	want := `prefix https://proxy.example.com/https/example.com/443/test`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyPreservesTrailingText(t *testing.T) {
	body := []byte(`https://example.com/test suffix`)
	want := `https://proxy.example.com/https/example.com/443/test suffix`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLQueryOnlyPath(t *testing.T) {
	got := rewriteSingleURL("https://example.com?x=1", "https://proxy.example.com")
	want := "https://proxy.example.com/https/example.com/443/?x=1"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyQueryOnlyPath(t *testing.T) {
	body := []byte(`https://example.com?x=1`)
	want := `https://proxy.example.com/https/example.com/443/?x=1`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLUppercaseSchemeUntouched(t *testing.T) {
	got := rewriteSingleURL("HTTPS://example.com", "https://proxy.example.com")
	if got != "HTTPS://example.com" {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, "HTTPS://example.com")
	}
}

func TestShouldRewriteEmbyResponseUsersPath(t *testing.T) {
	target := &target{Path: "Users/AuthenticateByName"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for users path")
	}
}

func TestShouldRewriteEmbyResponseSessionsPath(t *testing.T) {
	target := &target{Path: "Sessions/Playing"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for sessions path")
	}
}

func TestShouldRewriteEmbyResponseSystemPath(t *testing.T) {
	target := &target{Path: "System/Info/Public"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for system path")
	}
}

func TestShouldRewriteEmbyResponseMoviesPath(t *testing.T) {
	target := &target{Path: "Movies/Recommendations"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for movies path")
	}
}

func TestShouldRewriteEmbyResponseArtistsPath(t *testing.T) {
	target := &target{Path: "Artists/AlbumArtists"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for artists path")
	}
}

func TestShouldRewriteEmbyResponseAlbumsPath(t *testing.T) {
	target := &target{Path: "Albums/Latest"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for albums path")
	}
}

func TestShouldRewriteEmbyResponseShowsPath(t *testing.T) {
	target := &target{Path: "Shows/NextUp"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for shows path")
	}
}

func TestShouldRewriteEmbyResponseAudioPath(t *testing.T) {
	target := &target{Path: "Audio/Albums"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for audio path")
	}
}

func TestShouldRewriteEmbyResponseNotPlainBinary(t *testing.T) {
	target := &target{Path: "emby/Items"}
	if shouldRewriteEmbyResponse(target, "application/octet-stream") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for octet-stream")
	}
}

func TestRewriteBodyEmbeddedJSONURL(t *testing.T) {
	body := []byte(`{"Nested":{"Url":"https://example.com/a"}}`)
	want := `{"Nested":{"Url":"https://proxy.example.com/https/example.com/443/a"}}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyTextPlainMultipleLines(t *testing.T) {
	body := []byte("line1\nhttps://example.com/a\nline3")
	want := "line1\nhttps://proxy.example.com/https/example.com/443/a\nline3"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLWithExplicitHTTPSPort(t *testing.T) {
	got := rewriteSingleURL("https://example.com:8443/a", "https://proxy.example.com")
	want := "https://proxy.example.com/https/example.com/8443/a"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLWithExplicitHTTPPort(t *testing.T) {
	got := rewriteSingleURL("http://example.com:8080/a", "https://proxy.example.com")
	want := "https://proxy.example.com/http/example.com/8080/a"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyCurlyBraceTerminator(t *testing.T) {
	body := []byte(`{"u":"https://example.com/a"}`)
	want := `{"u":"https://proxy.example.com/https/example.com/443/a"}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyBracketTerminator(t *testing.T) {
	body := []byte(`["https://example.com/a"]`)
	want := `["https://proxy.example.com/https/example.com/443/a"]`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyBackslashTerminator(t *testing.T) {
	body := []byte("https://example.com/a\\suffix")
	want := "https://proxy.example.com/https/example.com/443/a\\suffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyCaretTerminator(t *testing.T) {
	body := []byte("https://example.com/a^suffix")
	want := "https://proxy.example.com/https/example.com/443/a^suffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyPipeTerminator(t *testing.T) {
	body := []byte("https://example.com/a|suffix")
	want := "https://proxy.example.com/https/example.com/443/a|suffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyTabTerminator(t *testing.T) {
	body := []byte("https://example.com/a\tsuffix")
	want := "https://proxy.example.com/https/example.com/443/a\tsuffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyCRLFTerminator(t *testing.T) {
	body := []byte("https://example.com/a\r\nsuffix")
	want := "https://proxy.example.com/https/example.com/443/a\r\nsuffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodySpaceTerminator(t *testing.T) {
	body := []byte("https://example.com/a suffix")
	want := "https://proxy.example.com/https/example.com/443/a suffix"
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyQuoteTerminator(t *testing.T) {
	body := []byte(`"https://example.com/a"`)
	want := `"https://proxy.example.com/https/example.com/443/a"`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodySingleQuoteTerminator(t *testing.T) {
	body := []byte(`'https://example.com/a'`)
	want := `'https://proxy.example.com/https/example.com/443/a'`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyAngleBracketTerminator(t *testing.T) {
	body := []byte(`<a>https://example.com/a</a>`)
	want := `<a>https://proxy.example.com/https/example.com/443/a</a>`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyParenthesisTerminator(t *testing.T) {
	body := []byte(`(https://example.com/a)`)
	want := `(https://proxy.example.com/https/example.com/443/a)`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyBraceTerminator(t *testing.T) {
	body := []byte(`{https://example.com/a}`)
	want := `{https://proxy.example.com/https/example.com/443/a}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodySquareBracketTerminator(t *testing.T) {
	body := []byte(`[https://example.com/a]`)
	want := `[https://proxy.example.com/https/example.com/443/a]`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyMultipleSchemes(t *testing.T) {
	body := []byte(`http://a.example.com/x https://b.example.com/y`)
	want := `https://proxy.example.com/http/a.example.com/80/x https://proxy.example.com/https/b.example.com/443/y`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyWithTextBeforeAndAfter(t *testing.T) {
	body := []byte(`a https://example.com/x b`)
	want := `a https://proxy.example.com/https/example.com/443/x b`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLRootOnly(t *testing.T) {
	got := rewriteSingleURL("https://example.com/", "https://proxy.example.com")
	want := "https://proxy.example.com/https/example.com/443/"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyRootOnlyURL(t *testing.T) {
	body := []byte(`https://example.com/`)
	want := `https://proxy.example.com/https/example.com/443/`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyDefaultPorts(t *testing.T) {
	body := []byte(`http://example.com/a https://example.com/b`)
	want := `https://proxy.example.com/http/example.com/80/a https://proxy.example.com/https/example.com/443/b`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestShouldRewriteEmbyResponseAlbumsLowercase(t *testing.T) {
	target := &target{Path: "albums/latest"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for albums/latest")
	}
}

func TestShouldRewriteEmbyResponseRootUnsupportedType(t *testing.T) {
	target := &target{Path: ""}
	if shouldRewriteEmbyResponse(target, "video/mp4") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for root video/mp4")
	}
}

func TestRewriteSingleURLHTTPSNoPathWithQuery(t *testing.T) {
	got := rewriteSingleURL("https://example.com?api_key=1", "https://proxy.example.com")
	want := "https://proxy.example.com/https/example.com/443/?api_key=1"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyHTTPSNoPathWithQuery(t *testing.T) {
	body := []byte(`https://example.com?api_key=1`)
	want := `https://proxy.example.com/https/example.com/443/?api_key=1`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteSingleURLHTTPNoPathWithQuery(t *testing.T) {
	got := rewriteSingleURL("http://example.com?api_key=1", "https://proxy.example.com")
	want := "https://proxy.example.com/http/example.com/80/?api_key=1"
	if got != want {
		t.Fatalf("rewriteSingleURL() = %q, want %q", got, want)
	}
}

func TestRewriteBodyHTTPNoPathWithQuery(t *testing.T) {
	body := []byte(`http://example.com?api_key=1`)
	want := `https://proxy.example.com/http/example.com/80/?api_key=1`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyEmbeddedInJSONList(t *testing.T) {
	body := []byte(`["https://a.example.com/x","http://b.example.com/y"]`)
	want := `["https://proxy.example.com/https/a.example.com/443/x","https://proxy.example.com/http/b.example.com/80/y"]`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyPreservesCommasAndJSON(t *testing.T) {
	body := []byte(`{"a":"https://a.example.com/x","b":1}`)
	want := `{"a":"https://proxy.example.com/https/a.example.com/443/x","b":1}`
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != want {
		t.Fatalf("rewriteBody() = %q, want %q", got, want)
	}
}

func TestRewriteBodyDoesNotTouchRelativeURL(t *testing.T) {
	body := []byte(`{"u":"/emby/Items/1"}`)
	if got := string(rewriteBody(body, "https://proxy.example.com")); got != string(body) {
		t.Fatalf("rewriteBody() = %q, want %q", got, string(body))
	}
}

func TestShouldRewriteBodyStillSupportsJavaScript(t *testing.T) {
	if !shouldRewriteBody("application/javascript") {
		t.Fatal("shouldRewriteBody() = false, want true for application/javascript")
	}
}

func TestShouldRewriteBodyRejectsBinary(t *testing.T) {
	if shouldRewriteBody("application/octet-stream") {
		t.Fatal("shouldRewriteBody() = true, want false for application/octet-stream")
	}
}

func TestShouldRewriteEmbyResponsePlaylistsLowercase(t *testing.T) {
	target := &target{Path: "playlists/1/items"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for playlists path")
	}
}

func TestShouldRewriteEmbyResponseArtistsLowercase(t *testing.T) {
	target := &target{Path: "artists/albumartists"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for artists path")
	}
}

func TestShouldRewriteEmbyResponseMoviesLowercase(t *testing.T) {
	target := &target{Path: "movies/recommendations"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for movies path")
	}
}

func TestShouldRewriteEmbyResponseShowsLowercase(t *testing.T) {
	target := &target{Path: "shows/nextup"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for shows path")
	}
}

func TestShouldRewriteEmbyResponseAudioLowercase(t *testing.T) {
	target := &target{Path: "audio/albums"}
	if shouldRewriteEmbyResponse(target, "application/json") {
		t.Fatal("shouldRewriteEmbyResponse() = true, want false for audio path")
	}
}

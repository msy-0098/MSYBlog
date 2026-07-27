package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIPLocationResolverCachesPublicIPLookup(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Query().Get("lang") != "zh" {
			t.Fatalf("expected Chinese response request, got %q", request.URL.RawQuery)
		}
		_, _ = writer.Write([]byte(`{"success":true,"country":"中国","region":"北京市","city":"北京市"}`))
	}))
	defer server.Close()

	resolver := NewIPLocationResolver(server.Client())
	resolver.endpoint = server.URL + "/"

	first, err := resolver.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookup first result: %v", err)
	}
	second, err := resolver.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookup cached result: %v", err)
	}
	if first.Location != "中国 · 北京市" || second != first {
		t.Fatalf("unexpected locations: first=%#v second=%#v", first, second)
	}
	if requests != 1 {
		t.Fatalf("expected one upstream request, got %d", requests)
	}
}

func TestIPLocationResolverDoesNotCallUpstreamForPrivateIP(t *testing.T) {
	resolver := NewIPLocationResolver(&http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("private IP must not reach the provider")
		return nil, nil
	})})

	result, err := resolver.Lookup(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatalf("lookup private IP: %v", err)
	}
	if result.Location != privateIPLocation {
		t.Fatalf("location = %q, want %q", result.Location, privateIPLocation)
	}
}

func TestIPLocationResolverRecognizesQuotaExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	resolver := NewIPLocationResolver(server.Client())
	resolver.endpoint = server.URL + "/"

	_, err := resolver.Lookup(context.Background(), "1.1.1.1")
	if !errors.Is(err, ErrIPLocationQuotaReached) {
		t.Fatalf("error = %v, want quota error", err)
	}
}

type roundTripper func(*http.Request) (*http.Response, error)

func (fn roundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
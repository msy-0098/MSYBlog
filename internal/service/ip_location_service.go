package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidIPAddress      = errors.New("invalid IP address")
	ErrIPLocationQuotaReached = errors.New("IP location quota reached")
	ErrIPLocationUnavailable = errors.New("IP location service unavailable")
)

const (
	privateIPLocation = "本地或内网地址"
	unknownIPLocation = "未知"
	ipLocationCacheTTL = 30 * 24 * time.Hour
)

type IPLocation struct {
	Location string `json:"location"`
}

type ipLocationCacheEntry struct {
	value     IPLocation
	expiresAt time.Time
}

type IPLocationResolver struct {
	client   *http.Client
	endpoint string
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]ipLocationCacheEntry
}

func NewIPLocationResolver(client *http.Client) *IPLocationResolver {
	if client == nil {
		client = &http.Client{Timeout: time.Second}
	}
	return &IPLocationResolver{
		client:   client,
		endpoint: "https://ipwho.is/",
		now:      time.Now,
		cache:    make(map[string]ipLocationCacheEntry),
	}
}

func (r *IPLocationResolver) Lookup(ctx context.Context, rawIP string) (IPLocation, error) {
	ip, err := normalizePublicIP(rawIP)
	if err != nil {
		return IPLocation{}, err
	}
	if ip == "" {
		return IPLocation{Location: privateIPLocation}, nil
	}
	if cached, ok := r.getCached(ip); ok {
		return cached, nil
	}

	requestURL, err := url.Parse(r.endpoint + url.PathEscape(ip))
	if err != nil {
		return IPLocation{}, ErrIPLocationUnavailable
	}
	query := requestURL.Query()
	query.Set("lang", "zh")
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return IPLocation{}, ErrIPLocationUnavailable
	}
	response, err := r.client.Do(req)
	if err != nil {
		return IPLocation{}, ErrIPLocationUnavailable
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusTooManyRequests {
		return IPLocation{}, ErrIPLocationQuotaReached
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return IPLocation{}, ErrIPLocationUnavailable
	}

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Country string `json:"country"`
		Region  string `json:"region"`
		City    string `json:"city"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&payload); err != nil {
		return IPLocation{}, ErrIPLocationUnavailable
	}
	if !payload.Success {
		if isQuotaMessage(payload.Message) {
			return IPLocation{}, ErrIPLocationQuotaReached
		}
		return IPLocation{}, ErrIPLocationUnavailable
	}

	location := IPLocation{Location: formatIPLocation(payload.Country, payload.Region, payload.City)}
	r.putCached(ip, location)
	return location, nil
}

func normalizePublicIP(rawIP string) (string, error) {
	parsed := net.ParseIP(strings.TrimSpace(rawIP))
	if parsed == nil {
		return "", ErrInvalidIPAddress
	}
	if parsed.IsLoopback() || parsed.IsPrivate() || parsed.IsUnspecified() || parsed.IsLinkLocalUnicast() || parsed.IsLinkLocalMulticast() || parsed.IsMulticast() {
		return "", nil
	}
	return parsed.String(), nil
}

func (r *IPLocationResolver) getCached(ip string) (IPLocation, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.cache[ip]
	if !ok || !entry.expiresAt.After(r.now()) {
		if ok {
			delete(r.cache, ip)
		}
		return IPLocation{}, false
	}
	return entry.value, true
}

func (r *IPLocationResolver) putCached(ip string, value IPLocation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache[ip] = ipLocationCacheEntry{value: value, expiresAt: r.now().Add(ipLocationCacheTTL)}
}

func formatIPLocation(parts ...string) string {
	unique := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || value == unknownIPLocation {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	if len(unique) == 0 {
		return unknownIPLocation
	}
	return strings.Join(unique, " · ")
}

func isQuotaMessage(message string) bool {
	value := strings.ToLower(strings.TrimSpace(message))
	return strings.Contains(value, "quota") || strings.Contains(value, "rate limit") || strings.Contains(value, "too many")
}

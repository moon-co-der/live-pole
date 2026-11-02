package services

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CountryFromIP asks ipapi.co for country-level data only. The caller never
// persists the IP, and private/local addresses are not sent to the provider.
func CountryFromIP(ctx context.Context, address string) string {
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 900*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(lookupCtx, http.MethodGet, "https://ipapi.co/"+url.PathEscape(ip.String())+"/country/", nil)
	if err != nil {
		return ""
	}
	client := &http.Client{Timeout: time.Second}
	response, err := client.Do(request)
	if err != nil {
		return ""
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 16))
	if err != nil {
		return ""
	}
	country := strings.ToUpper(strings.TrimSpace(string(body)))
	if len(country) != 2 {
		return ""
	}
	return country
}

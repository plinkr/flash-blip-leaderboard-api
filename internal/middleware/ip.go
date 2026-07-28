package middleware

import (
	"net"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// GetClientIP extracts the real client IP address checking proxies (Cloudflare, X-Real-IP, X-Forwarded-For)
// with fallback to Fiber c.IP() and RemoteAddr.
func GetClientIP(c *fiber.Ctx) string {
	if ip := strings.TrimSpace(c.Get("CF-Connecting-IP")); ip != "" && isValidIP(ip) {
		return ip
	}

	if ip := strings.TrimSpace(c.Get("X-Real-IP")); ip != "" && isValidIP(ip) {
		return ip
	}

	if xff := c.Get(fiber.HeaderXForwardedFor); xff != "" {
		for ipStr := range strings.SplitSeq(xff, ",") {
			if ip := strings.TrimSpace(ipStr); ip != "" && isValidIP(ip) {
				return ip
			}
		}
	}

	if ip := strings.TrimSpace(c.IP()); ip != "" && ip != "<nil>" && isValidIP(ip) {
		return ip
	}

	remoteAddr := c.Context().RemoteAddr().String()
	if remoteAddr != "" && remoteAddr != "<nil>" {
		if host, _, err := net.SplitHostPort(remoteAddr); err == nil && isValidIP(host) {
			return host
		}
		if isValidIP(remoteAddr) {
			return remoteAddr
		}
	}

	return ""
}

func isValidIP(ipStr string) bool {
	return net.ParseIP(ipStr) != nil
}

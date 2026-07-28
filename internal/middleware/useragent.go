package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func UserAgent(allowed []string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		if len(allowed) == 0 || c.Method() == fiber.MethodOptions {
			return c.Next()
		}

		ua := c.Get("User-Agent")

		// Any standard web browser
		if strings.HasPrefix(ua, "Mozilla/5.0") {
			return c.Next()
		}

		for _, allowedUA := range allowed {
			if allowedUA == "*" || ua == allowedUA {
				return c.Next()
			}
		}

		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "Forbidden: unauthorized client",
		})
	}
}

package parser

import (
	"encoding/base64"
	"regexp"
	"strings"
)

// کامپایل عبارات منظم در سطح پکیج برای بالاترین پرفورمنس
var (
	titleRegex       = regexp.MustCompile(`(?i)<title[^>]*>(.*?)</title>`)
	panelSuffixRegex = regexp.MustCompile(`(?i)\s*-?\s*Sub\s+Info.*$`)
	doroodRegex      = regexp.MustCompile(`(?i)درود\s+([^\s<]+)`)
)

// ExtractCleanTitle همان تابع قوی شماست که حالا با سرعت و دقت Go بازنویسی شده
func ExtractCleanTitle(htmlContent string) string {
	if len(htmlContent) < 10 {
		return ""
	}

	// استخراج از تگ title
	matches := titleRegex.FindStringSubmatch(htmlContent)
	if len(matches) > 1 {
		title := matches[1]
		// حذف پسوندهای اضافی پنل‌ها
		title = panelSuffixRegex.ReplaceAllString(title, "")
		title = strings.TrimSpace(title)
		
		if title != "" {
			return title
		}
	}

	// Fallback: جستجوی عبارت درود
	nameMatches := doroodRegex.FindStringSubmatch(htmlContent)
	if len(nameMatches) > 1 {
		return strings.TrimSpace(nameMatches[1])
	}

	return ""
}

// DecodeConfigs بررسی می‌کند که آیا متن Base64 است یا خیر، و آن را دیکد می‌کند
func DecodeConfigs(content string) string {
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		// اگر Base64 نبود، همان متن خام را برمی‌گرداند
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(string(decoded))
}

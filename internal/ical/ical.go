// Package ical builds minimal RFC 5545 iCalendar (.ics) feeds. It only
// implements what this tool needs: a VCALENDAR of all-day VEVENTs.
package ical

import (
	"strings"
	"time"
)

// Event is one all-day calendar entry.
type Event struct {
	UID         string
	Summary     string
	Description string
	URL         string
	Date        time.Time // day only; rendered as an all-day event
}

// Build renders events into a full VCALENDAR document. calName is used for
// X-WR-CALNAME, which most clients show as the subscribed calendar's name.
func Build(calName string, events []Event) []byte {
	var b strings.Builder
	now := time.Now().UTC().Format("20060102T150405Z")

	writeLine(&b, "BEGIN:VCALENDAR")
	writeLine(&b, "VERSION:2.0")
	writeLine(&b, "PRODID:-//ryot-calendar-sync//upcoming-releases//EN")
	writeLine(&b, "CALSCALE:GREGORIAN")
	writeLine(&b, "METHOD:PUBLISH")
	writeLine(&b, "X-WR-CALNAME:"+escapeText(calName))
	writeLine(&b, "X-PUBLISHED-TTL:PT12H")

	for _, ev := range events {
		writeLine(&b, "BEGIN:VEVENT")
		writeLine(&b, "UID:"+escapeText(ev.UID)+"@ryot-calendar-sync")
		writeLine(&b, "DTSTAMP:"+now)
		writeLine(&b, "DTSTART;VALUE=DATE:"+ev.Date.Format("20060102"))
		writeLine(&b, "DTEND;VALUE=DATE:"+ev.Date.AddDate(0, 0, 1).Format("20060102"))
		writeLine(&b, "SUMMARY:"+escapeText(ev.Summary))
		if ev.Description != "" {
			writeLine(&b, "DESCRIPTION:"+escapeText(ev.Description))
		}
		if ev.URL != "" {
			writeLine(&b, "URL:"+escapeText(ev.URL))
		}
		writeLine(&b, "TRANSP:TRANSPARENT")
		writeLine(&b, "END:VEVENT")
	}

	writeLine(&b, "END:VCALENDAR")
	return []byte(b.String())
}

// escapeText escapes the handful of characters RFC 5545 requires escaping
// in TEXT values: backslash, semicolon, comma, and newlines.
func escapeText(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`;`, `\;`,
		`,`, `\,`,
		"\r\n", `\n`,
		"\n", `\n`,
	)
	return r.Replace(s)
}

// writeLine appends a CRLF-terminated, RFC 5545 line-folded content line.
// Lines are folded at 75 octets, with continuation lines starting with a
// single space, per the spec.
func writeLine(b *strings.Builder, line string) {
	const maxLineLen = 75
	if len(line) <= maxLineLen {
		b.WriteString(line)
		b.WriteString("\r\n")
		return
	}
	remaining := line
	first := true
	for len(remaining) > 0 {
		limit := maxLineLen
		if !first {
			limit = maxLineLen - 1
		}
		if limit > len(remaining) {
			limit = len(remaining)
		}
		// Avoid splitting a UTF-8 rune in half.
		for limit > 0 && isUTF8Continuation(remaining[limit-1]) {
			limit--
		}
		if !first {
			b.WriteString(" ")
		}
		b.WriteString(remaining[:limit])
		b.WriteString("\r\n")
		remaining = remaining[limit:]
		first = false
	}
}

func isUTF8Continuation(c byte) bool {
	return c&0xC0 == 0x80
}

// ContentType is the MIME type calendar clients expect for .ics feeds.
const ContentType = "text/calendar; charset=utf-8"

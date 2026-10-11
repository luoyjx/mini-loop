package protocol

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
)

// Matches the installed SDK's _datetime_parse grammar. Invalid observations are
// retained, as SDK construction falls back when datetime validation fails.
var sdkReplyDatetime = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})[T ](\d{1,2}):(\d{1,2})(?::(\d{1,2})(?:\.(\d{1,6})\d{0,6})?)?(Z|[+-]\d{2}(?::?\d{2})?)?$`)

func normalizeSDKReplyExpiry(value jsonvalue.Value) jsonvalue.Value {
	text, textual := value.Text()
	numeric := text
	if integer, ok := value.Integer(); ok {
		numeric = integer
	}
	if number, ok := value.Float(); ok {
		numeric = strconv.FormatFloat(number, 'g', -1, 64)
	}
	if boolean, ok := value.Bool(); ok {
		if boolean {
			numeric = "1"
		} else {
			numeric = "0"
		}
	}
	if number, err := strconv.ParseFloat(strings.TrimSpace(numeric), 64); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
		if number > 3e20 {
			return jsonvalue.TextValue("9999-12-31T23:59:59.999999")
		}
		if number < -3e20 {
			return jsonvalue.TextValue("0001-01-01T00:00:00")
		}
		for math.Abs(number) > 2e10 {
			number /= 1000
		}
		whole, fraction := math.Modf(number)
		stamp := time.Unix(int64(whole), int64(math.RoundToEven(fraction*1e6))*1000).UTC()
		if stamp.Year() < 1 || stamp.Year() > 9999 {
			return value
		}
		return jsonvalue.TextValue(formatSDKReplyDatetime(stamp, true))
	}
	if !textual {
		return value
	}
	parts := sdkReplyDatetime.FindStringSubmatch(text)
	if parts == nil {
		return value
	}
	nums := [6]int{}
	for i := range nums {
		if parts[i+1] != "" {
			nums[i], _ = strconv.Atoi(parts[i+1])
		}
	}
	if nums[0] < 1 || nums[0] > 9999 || nums[1] < 1 || nums[1] > 12 || nums[2] < 1 || nums[2] > 31 || nums[3] > 23 || nums[4] > 59 || nums[5] > 59 {
		return value
	}
	micro := 0
	if parts[7] != "" {
		micro, _ = strconv.Atoi(parts[7] + strings.Repeat("0", 6-len(parts[7])))
	}
	zone := parts[8]
	offset := 0
	if zone != "" && zone != "Z" {
		hours, _ := strconv.Atoi(zone[1:3])
		mins := 0
		if len(zone) > 3 {
			mins, _ = strconv.Atoi(zone[len(zone)-2:])
		}
		offset = (hours*60 + mins) * 60
		if offset >= 86400 {
			return value
		}
		if zone[0] == '-' {
			offset = -offset
		}
	}
	stamp := time.Date(nums[0], time.Month(nums[1]), nums[2], nums[3], nums[4], nums[5], micro*1000, time.FixedZone("", offset))
	if stamp.Month() != time.Month(nums[1]) || stamp.Day() != nums[2] {
		return value
	}
	return jsonvalue.TextValue(formatSDKReplyDatetime(stamp, zone != ""))
}
func formatSDKReplyDatetime(stamp time.Time, zoned bool) string {
	text := stamp.Format("2006-01-02T15:04:05")
	if stamp.Nanosecond() != 0 {
		text += stamp.Format(".000000")
	}
	if zoned {
		_, offset := stamp.Zone()
		if offset == 0 {
			text += "Z"
		} else {
			text += stamp.Format("-07:00")
		}
	}
	return text
}

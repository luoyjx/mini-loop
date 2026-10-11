package protocol

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/luoyjx/mini-loop/go/internal/jsonvalue"
	"github.com/luoyjx/mini-loop/go/internal/pytext"
)

// Matches the installed SDK's _datetime_parse grammar. Invalid observations are
// retained, as SDK construction falls back when datetime validation fails.
var sdkReplyDatetime = regexp.MustCompile(`^(\d{4})-(\d{1,2})-(\d{1,2})[T ](\d{1,2}):(\d{1,2})(?::(\d{1,2})(?:\.(\d{1,6})\d{0,6})?)?(Z|[+-]\d{2}(?::?\d{2})?)?$`)
var validatedReplyDatetime = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})[Tt _](\d{2}):(\d{2})(?::(\d{2})(?:[.,](\d+))?)?([Zz]|[+-]\d{2}:?\d{2})?$`)
var validatedReplyNumber = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)$`)
var pythonReplyNumber = regexp.MustCompile(`^[+-]?(?:(?:[0-9](?:_?[0-9])*(?:\.(?:[0-9](?:_?[0-9])*)?)?|\.[0-9](?:_?[0-9])*)(?:[eE][+-]?[0-9](?:_?[0-9])*)?|(?i:inf(?:inity)?|nan))$`)

// Optional[Container] first validates the whole object. A non-text or absent ID
// selects SDK loose construction, including Python timedelta's rounding behavior.
func normalizeSDKReplyExpiry(value jsonvalue.Value, validContainerID bool) jsonvalue.Value {
	if validContainerID {
		if parsed, ok := validatedReplyExpiry(value); ok {
			return parsed
		}
	}
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
	numeric = strings.TrimFunc(replyDecimalText(numeric), func(r rune) bool {
		return pytext.Space(r) && !(r >= 0x1c && r <= 0x1f)
	})
	if number, ok := pythonReplyFloat(numeric); ok && !math.IsNaN(number) {
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
	return parsedReplyCalendar(value, sdkReplyDatetime.FindStringSubmatch(strings.TrimSuffix(replyDecimalText(text), "\n")), false)
}

func parsedReplyCalendar(value jsonvalue.Value, parts []string, strict bool) jsonvalue.Value {
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
		fraction := parts[7]
		if len(fraction) > 6 {
			fraction = fraction[:6]
		}
		micro, _ = strconv.Atoi(fraction + strings.Repeat("0", 6-len(fraction)))
	}
	zone := strings.ToUpper(parts[8])
	offset := 0
	if zone != "" && zone != "Z" {
		hours, _ := strconv.Atoi(zone[1:3])
		mins := 0
		if len(zone) > 3 {
			mins, _ = strconv.Atoi(zone[len(zone)-2:])
		}
		if strict && mins > 59 {
			return value
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

func replyDecimalText(text string) string {
	return strings.Map(func(r rune) rune {
		if digit, ok := pytext.DecimalDigit(r); ok {
			return rune(digit)
		}
		return r
	}, text)
}

func pythonReplyFloat(text string) (float64, bool) {
	if !pythonReplyNumber.MatchString(text) {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.ReplaceAll(text, "_", ""), 64)
	// Python float accepts overflow as infinity; SDK loose construction saturates it.
	return number, err == nil || math.IsInf(number, 0)
}

func validatedReplyExpiry(value jsonvalue.Value) (jsonvalue.Value, bool) {
	text, textual := value.Text()
	if textual && !validatedReplyNumber.MatchString(text) {
		calendar := text
		if len(text) == 10 {
			calendar += "T00:00:00"
		}
		parsed := parsedReplyCalendar(value, validatedReplyDatetime.FindStringSubmatch(calendar), true)
		return parsed, !parsed.PythonEqual(value)
	}
	if integer, ok := value.Integer(); ok {
		text = integer
	}
	if number, ok := value.Float(); ok {
		stamp, valid := validatedReplyTimestamp(number, false)
		return jsonvalue.TextValue(formatSDKReplyDatetime(stamp, true)), valid
	}
	if !textual && value.Kind() != jsonvalue.Integer {
		return jsonvalue.Value{}, false
	}
	number, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return jsonvalue.Value{}, false
	}
	stamp, valid := validatedReplyTimestamp(number, textual)
	return jsonvalue.TextValue(formatSDKReplyDatetime(stamp, true)), valid
}

func validatedReplyTimestamp(number float64, textual bool) (time.Time, bool) {
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) >= 9e18 {
		return time.Time{}, false
	}
	// speedate's text and Python-float paths intentionally differ for negatives.
	var whole, micro float64
	if textual {
		if math.Abs(number) > 2e10 {
			number /= 1000
		}
		whole = math.Floor(number)
		micro = math.Round((number - whole) * 1e6)
	} else {
		whole = math.Floor(number)
		_, fraction := math.Modf(number)
		scale := 1e6
		if math.Abs(number) > 2e10 {
			scale = 1e3
		}
		micro = math.Round(math.Abs(fraction) * scale)
	}
	seconds := int64(whole)
	if math.Abs(whole) > 2e10 {
		// Euclidean remainder matches speedate's negative millisecond handling.
		quotient, remainder := seconds/1000, seconds%1000
		if remainder < 0 {
			quotient--
			remainder += 1000
		}
		seconds = quotient
		micro += float64(remainder * 1000)
	}
	stamp := time.Unix(seconds, int64(micro)*1000).UTC()
	if stamp.Year() < 1600 || stamp.Year() > 9999 {
		return time.Time{}, false
	}
	return stamp, true
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

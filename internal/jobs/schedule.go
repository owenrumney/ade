package jobs

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// minInterval is the floor under an interval schedule. A job is a process on
// the subscription, and one a minute is a way to spend a window by accident.
const minInterval = time.Minute

// searchDays is how far Next looks before giving up. A cron expression that
// matches nothing — 31 February — would otherwise loop forever.
const searchDays = 5 * 366

// Manual is the schedule of a job with no clock behind it: it fires when
// somebody presses Run, and at no other time.
const Manual = "manual"

// manualForms are what a definition may write for Manual.
var manualForms = map[string]bool{"manual": true, "adhoc": true, "ad hoc": true, "on demand": true}

// Schedule is when a job fires. Zero is never, which no valid definition has.
type Schedule struct {
	spec   string
	every  time.Duration
	manual bool
	sets   [5]field
}

// field is one cron column as a set of the values it matches.
type field struct {
	bits uint64
	star bool
}

// bounds are each column's legal range, in cron order.
var bounds = [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}

// names lets a month or a weekday be written rather than numbered.
var names = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6,
}

// ParseSchedule reads a five-field cron expression, or an interval written as
// a Go duration with or without an "@every" in front of it.
func ParseSchedule(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Schedule{}, errors.New("no schedule: a cron expression like \"0 8 * * 1-5\", an interval like \"@every 2h\", or \"manual\" for a job you run yourself")
	}
	// Before the interval reader, which would take "manual" for a duration.
	if manualForms[strings.ToLower(spec)] {
		return Schedule{spec: Manual, manual: true}, nil
	}
	if rest, ok := interval(spec); ok {
		every, err := time.ParseDuration(rest)
		if err != nil {
			return Schedule{}, fmt.Errorf("interval %q: %w", rest, err)
		}
		if every < minInterval {
			return Schedule{}, fmt.Errorf("interval %s is under the %s floor", every, minInterval)
		}
		return Schedule{spec: spec, every: every}, nil
	}

	columns := strings.Fields(spec)
	if len(columns) != 5 {
		return Schedule{}, fmt.Errorf("schedule %q has %d fields; cron takes 5 (minute hour day-of-month month day-of-week)", spec, len(columns))
	}
	out := Schedule{spec: spec}
	for i, column := range columns {
		set, err := parseField(column, bounds[i][0], bounds[i][1])
		if err != nil {
			return Schedule{}, fmt.Errorf("schedule %q field %d: %w", spec, i+1, err)
		}
		out.sets[i] = set
	}
	return out, nil
}

// interval reads the interval forms and reports the duration part.
func interval(spec string) (string, bool) {
	for _, prefix := range []string{"@every ", "every "} {
		if rest, ok := cutPrefix(spec, prefix); ok {
			return rest, true
		}
	}
	// A bare duration: anything with no space in it that is not a cron column.
	if !strings.ContainsAny(spec, " \t*,/") {
		return spec, true
	}
	return "", false
}

func cutPrefix(s, prefix string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(s), prefix) {
		return "", false
	}
	return strings.TrimSpace(s[len(prefix):]), true
}

// Spec is the schedule as written.
func (s Schedule) Spec() string { return s.spec }

// Interval is the duration of an interval schedule, and false for a cron one.
func (s Schedule) Interval() (time.Duration, bool) { return s.every, s.every > 0 }

// Manual is whether nothing but a person fires this job.
func (s Schedule) Manual() bool { return s.manual }

// Next is the first fire strictly after t, or zero when nothing matches.
func (s Schedule) Next(t time.Time) time.Time {
	if s.manual {
		return time.Time{}
	}
	if s.every > 0 {
		return t.Add(s.every).Truncate(time.Second)
	}
	if s.spec == "" {
		return time.Time{}
	}
	// From the next whole minute: a schedule matching this one has already
	// been fired for, and Next must move forward.
	at := t.Truncate(time.Minute).Add(time.Minute)
	limit := at.AddDate(0, 0, searchDays)
	for at.Before(limit) {
		if !s.dayMatches(at) {
			at = time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location()).AddDate(0, 0, 1)
			continue
		}
		if s.sets[1].has(at.Hour()) && s.sets[0].has(at.Minute()) {
			return at
		}
		at = at.Add(time.Minute)
	}
	return time.Time{}
}

// dayMatches is cron's day rule: day-of-month and day-of-week are ORed when
// both are restricted, and ANDed with the month either way.
func (s Schedule) dayMatches(at time.Time) bool {
	if !s.sets[3].has(int(at.Month())) {
		return false
	}
	dom, dow := s.sets[2], s.sets[4]
	if dom.star && dow.star {
		return true
	}
	if dom.star {
		return dow.has(int(at.Weekday()))
	}
	if dow.star {
		return dom.has(at.Day())
	}
	return dom.has(at.Day()) || dow.has(int(at.Weekday()))
}

func (f field) has(v int) bool { return v >= 0 && v < 64 && f.bits&(1<<uint(v)) != 0 }

// parseField reads one column: "*", "5", "1-5", "*/15", "1-5/2", or a comma
// separated list of those.
func parseField(column string, min, max int) (field, error) {
	out := field{star: column == "*" || strings.HasPrefix(column, "*/")}
	for _, part := range strings.Split(column, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return field{}, errors.New("empty value")
		}
		spread := 1
		if body, step, found := strings.Cut(part, "/"); found {
			n, err := strconv.Atoi(strings.TrimSpace(step))
			if err != nil || n < 1 {
				return field{}, fmt.Errorf("step %q is not a positive number", step)
			}
			part, spread = body, n
		}
		low, high, err := span(part, min, max)
		if err != nil {
			return field{}, err
		}
		for v := low; v <= high; v += spread {
			out.bits |= 1 << uint(v)
		}
	}
	if out.bits == 0 {
		return field{}, errors.New("matches nothing")
	}
	return out, nil
}

// span is the range a part covers: the whole column for "*", one value, or
// low to high.
func span(part string, min, max int) (int, int, error) {
	if part == "*" {
		return min, max, nil
	}
	lowText, highText, ranged := strings.Cut(part, "-")
	low, err := value(lowText, min, max)
	if err != nil {
		return 0, 0, err
	}
	if !ranged {
		return low, low, nil
	}
	high, err := value(highText, min, max)
	if err != nil {
		return 0, 0, err
	}
	if high < low {
		return 0, 0, fmt.Errorf("range %q runs backwards", part)
	}
	return low, high, nil
}

func value(text string, min, max int) (int, error) {
	text = strings.TrimSpace(text)
	if named, ok := names[strings.ToLower(text)]; ok {
		return named, nil
	}
	v, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", text)
	}
	// Cron writes Sunday as either end of the week.
	if max == 6 && v == 7 {
		v = 0
	}
	if v < min || v > max {
		return 0, fmt.Errorf("%d is outside %d-%d", v, min, max)
	}
	return v, nil
}

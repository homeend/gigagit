//go:build linux

package sessionreg

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// procStart is when pid started: /proc/<pid>/stat field 22 (clock ticks
// since boot, USER_HZ = 100 on every Linux gg runs on) plus /proc/stat btime.
func procStart(pid int) (time.Time, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, false
	}
	s := string(raw)
	i := strings.LastIndexByte(s, ')') // comm may hold spaces and parens
	if i < 0 {
		return time.Time{}, false
	}
	fields := strings.Fields(s[i+1:]) // fields[0] is field 3 (state)
	if len(fields) < 20 {
		return time.Time{}, false
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64) // field 22
	if err != nil {
		return time.Time{}, false
	}
	boot, ok := bootTime()
	if !ok {
		return time.Time{}, false
	}
	return boot.Add(time.Duration(ticks) * (time.Second / 100)), true
}

func bootTime() (time.Time, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, false
			}
			return time.Unix(n, 0), true
		}
	}
	return time.Time{}, false
}

func procStartKnown() bool { _, ok := procStart(os.Getpid()); return ok }

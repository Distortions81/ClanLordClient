package main

import (
	"regexp"
	"strconv"
	"sync"
)

type NightInfo struct {
	mu       sync.Mutex
	Level    int
	SunAngle int
	Cloudy   bool
}

var gNight NightInfo

var nightRE = regexp.MustCompile(`^/nt ([0-9]+) /sa ([-0-9]+) /cl ([01])`)

func parseNightCommand(s string) bool {
	m := nightRE.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	lvl, _ := strconv.Atoi(m[1])
	sa, _ := strconv.Atoi(m[2])
	cloudy := m[3] != "0"
	gNight.mu.Lock()
	gNight.Level = lvl
	gNight.SunAngle = sa
	gNight.Cloudy = cloudy
	gNight.mu.Unlock()
	return true
}

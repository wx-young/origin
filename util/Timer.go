package util

import "time"

type Timer struct {
	lasttime     int64
	timeinterval int64
}

func (slf *Timer) SetupTimer(ms int32) {

	slf.lasttime = time.Now().UnixNano()
	slf.timeinterval = int64(ms) * 1e6
}

func (slf *Timer) SetupTimerOnlyInterval(ms int32) {
	slf.timeinterval = int64(ms) * 1e6
}

func (slf *Timer) SetupTimerTheHour(hour int32) {
	timeNow := time.Now()
	nt := timeNow.Truncate(time.Hour * 1)
	slf.lasttime = nt.UnixNano()
	slf.timeinterval = int64(hour) * 3600 * 1e9
}

func (slf *Timer) CheckTimeOut() bool {
	now := time.Now().UnixNano()
	if now-slf.lasttime > slf.timeinterval {
		slf.lasttime = now

		return true
	}

	return false
}

func (slf *Timer) CheckTimeOutHour() bool {
	now := time.Now().UnixNano()
	if now-slf.lasttime > slf.timeinterval {
		timeNow := time.Now()
		nt := timeNow.Truncate(time.Hour * 1)
		slf.lasttime = nt.UnixNano()

		return true
	}

	return false
}

func (slf *Timer) Reset() {
	slf.lasttime = time.Now().UnixNano()
}

package apptime

import "time"

var Beijing = time.FixedZone("Asia/Shanghai", 8*60*60)

func InBeijing(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	return value.In(Beijing)
}

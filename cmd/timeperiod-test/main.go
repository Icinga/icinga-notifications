package main

import (
	"fmt"
	"github.com/icinga/noma/internal/timeperiod"
	"time"
)

func main() {
	base := time.Now()

	for _, p := range timeperiod.TimePeriods {
		initialState := "inactive"
		if p.Contains(base) {
			initialState = "active"
		}
		fmt.Printf("Time Period %q (currently %s):\n", p.Name, initialState)

		transitions := 0

		for t := base; transitions < 5 && t.Before(base.Add(7*24*time.Hour)); t = p.NextTransition(t) {
			containsBefore := p.Contains(t.Add(-time.Nanosecond))
			containsAfter := p.Contains(t)
			if containsAfter != containsBefore {
				if containsAfter {
					fmt.Printf("  enters at %v\n", t)
				} else {
					fmt.Printf("  exits at %v\n", t)
				}
				transitions++
			}
		}

		if transitions == 0 {
			fmt.Println("  no transition found within a week")
		}
	}
}

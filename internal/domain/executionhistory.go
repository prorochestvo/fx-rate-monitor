package domain

import "time"

type ExecutionHistory struct {
	ID         string
	SourceName string
	Success    bool
	Error      string
	Timestamp  time.Time
}

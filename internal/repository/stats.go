package repository

// StatsResult holds the global application statistics.
type StatsResult struct {
	SourcesTotal  int64
	SourcesActive int64
	ErrorsTotal   int64
}

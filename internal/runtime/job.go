package runtime

// Job is the fully-resolved, OS-neutral description of a Service that keep
// hands an adapter to render. It is what the Config plus keep's own
// conventions (the pinned keep path, log paths, markers) resolve to, with no
// runtime-specific fields.
type Job struct {
	Label             string
	ProgramArguments  []string
	RunAtLoad         bool
	KeepAlive         bool
	StandardOutPath   string
	StandardErrorPath string
	StartInterval     int                // seconds; 0 == unset
	StartCalendar     []CalendarInterval // calendar fire times

	// Marker fields, embedded in every artifact so keep recognizes its own
	// output independently of the label (D19).
	Service     string
	KeepVersion string
	KeepPath    string
}

// CalendarInterval is one calendar fire time. A nil field means "any".
type CalendarInterval struct {
	Minute  *int
	Hour    *int
	Day     *int
	Weekday *int
	Month   *int
}

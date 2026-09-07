package plumber

type StatusStopCases struct {
	handled bool
	result  bool
}

type (
	JobFn func(job Job) Job
)

// Jobber is anything that can be run as a job of a flow.
type Jobber interface {
	Job() Job
}

// TaskLister is anything that can be combined with other task lists through CombineTaskLists.
type TaskLister interface {
	JobBefore() Job
	Job() Job
	JobAfter() Job
}

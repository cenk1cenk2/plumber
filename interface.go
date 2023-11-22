package plumber

type StatusStopCases struct {
	handled bool
	result  bool
}

type (
	JobFn func(job Job) Job
)

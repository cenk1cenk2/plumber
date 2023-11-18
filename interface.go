package plumber

type StatusStopCases struct {
	handled bool
	result  bool
}

type (
	JobWrapperFn func(job Job) Job
)

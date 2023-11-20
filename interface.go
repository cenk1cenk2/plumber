package plumber

type StatusStopCases struct {
	handled bool
	result  bool
}

type (
	JobWrapperFn[With any] func(job Job, t With) Job
)

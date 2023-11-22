package plumber

type StatusStopCases struct {
	handled bool
	result  bool
}

type (
	JobWrapperFn[With any] func(job Job, t With) Job
	JobParserFn[With any]  func(t With) Job
	JobFn                  func(job Job) Job
)

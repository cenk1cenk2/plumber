package plumber

import (
	"time"

	"github.com/workanator/go-floc/v3"
	"github.com/workanator/go-floc/v3/run"
)

func JobBackground(job floc.Job) floc.Job {
	return run.Background(job)
}

func JobLoop(job floc.Job) floc.Job {
	return run.Loop(job)
}

func JobDelay(delay time.Duration, job floc.Job) floc.Job {
	return run.Delay(delay, job)
}

func JobIf(predicate floc.Predicate, job floc.Job) floc.Job {
	return run.If(predicate, job)
}

func JobIfNot(predicate floc.Predicate, jobs ...floc.Job) floc.Job {
	return run.IfNot(predicate, jobs...)
}

func JobElse(job floc.Job) floc.Job {
	return run.Else(job)
}

func JobWait(predicate floc.Predicate, sleep time.Duration) floc.Job {
	return run.Wait(predicate, sleep)
}

func JobWhile(predicate floc.Predicate, job floc.Job) floc.Job {
	return run.While(predicate, job)
}

func JobParallel(jobs ...floc.Job) floc.Job {
	return run.Parallel(jobs...)
}

func JobSequence(jobs ...floc.Job) floc.Job {
	return run.Sequence(jobs...)
}

func JobRepeat(times int, job floc.Job) floc.Job {
	return run.Repeat(times, job)
}

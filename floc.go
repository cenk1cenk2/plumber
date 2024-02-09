package plumber

import (
	"fmt"
	"time"

	"github.com/workanator/go-floc/v3"
	"github.com/workanator/go-floc/v3/guard"
	"github.com/workanator/go-floc/v3/pred"
	"github.com/workanator/go-floc/v3/run"
)

type (
	Job          = floc.Job
	JobPredicate = floc.Predicate
	Result       = floc.Result
	ResultMask   = floc.ResultMask

	GuardHandlerFn[Pipe TaskListData] func(*TaskList[Pipe])
)

const (
	TASK_ANY       Result = floc.None
	TASK_COMPLETED Result = floc.Completed
	TASK_CANCELLED Result = floc.Canceled
	TASK_FAILED    Result = floc.Failed
)

// Creates a new floc predicate out of the given conditions.
func (tl *TaskList[Pipe]) Predicate(fn TaskListPredicateFn[Pipe]) JobPredicate {
	return func(_ floc.Context) bool {
		return fn(tl)
	}
}

/*
JobBackground starts the job in it's own goroutine. The function does not
track the lifecycle of the job started and does no synchronization with it
therefore the job running in background may remain active even if the flow
is finished. The function assumes the job is aware of the flow state and/or
synchronization and termination of it is implemented outside.

	floc.Run(run.Background(
		func(ctx floc.Context, ctrl floc.Control) error {
			for !ctrl.IsFinished() {
				fmt.Println(time.Now())
			}

			return nil
		}
	})

Summary:
  - Run jobs in goroutines : YES
  - Wait all jobs finish   : NO
  - Run order              : SINGLE

Diagram:

	--+----------->
	  |
	  +-->[JOB]
*/
func (tl *TaskList[Pipe]) JobBackground(job Job) Job {
	return run.Background(job)
}

func (tl *TaskList[Pipe]) CreateJob(fn func(tl *TaskList[Pipe]) error) Job {
	return func(_ floc.Context, _ floc.Control) error {
		return fn(tl)
	}
}

func (tl *TaskList[Pipe]) CreateBasicJob(fn func() error) Job {
	return func(_ floc.Context, _ floc.Control) error {
		return fn()
	}
}

/*
JobLoop repeats running the job forever.

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	  +----------+
	  |          |
	  V          |
	----->[JOB]--+
*/
func (tl *TaskList[Pipe]) JobLoop(job Job) Job {
	return run.Loop(job)
}

func (tl *TaskList[Pipe]) JobLoopWithWaitAfter(job Job, delay time.Duration) Job {
	return run.Loop(
		run.Sequence(
			job,
			run.Delay(delay, tl.CreateBasicJob(func() error {
				return nil
			})),
		),
	)
}

/*
JobDelay does delay before starting the job.

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	--(DELAY)-->[JOB]-->
*/
func (tl *TaskList[Pipe]) JobDelay(job Job, delay time.Duration) Job {
	return run.Delay(delay, job)
}

/*
JobIf runs the first job if the condition is met and runs
the second job, if it's passed, if the condition is not met.
The function panics if no or more than two jobs are given.

For expressiveness Then() and Else() can be used.

	flow := run.If(testSomething,
	  run.Then(doSomething),
	  run.Else(doSomethingElse),
	)

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	                    +----->[JOB_1]---+
	                    | YES            |
	--(CONDITION MET?)--+                +-->
	                    | NO             |
	                    +----->[JOB_2]---+
*/
func (tl *TaskList[Pipe]) JobIf(predicate JobPredicate, jobs ...Job) Job {
	return run.If(predicate, jobs...)
}

/*
JobIfNot runs the first job if the condition is not met and runs
the second job, if it's passed, if the condition is met.
The function panics if no or more than two jobs are given.

For expressiveness Then() and Else() can be used.

	flow := run.IfNot(testSomething,
	  run.Then(doSomething),
	  run.Else(doSomethingElse),
	)

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	                    +----->[JOB_1]---+
	                    | NO             |
	--(CONDITION MET?)--+                +-->
	                    | YES            |
	                    +----->[JOB_2]---+
*/
func (tl *TaskList[Pipe]) JobIfNot(predicate JobPredicate, jobs ...Job) Job {
	return run.IfNot(predicate, jobs...)
}

/*
Then just returns the job unmodified. Then is used for expressiveness
and can be omitted.

Summary:
  - Run jobs in goroutines : N/A
  - Wait all jobs finish   : N/A
  - Run order              : N/A

Diagram:

	----[JOB]--->
*/
func (tl *TaskList[Pipe]) JobThen(job Job) Job {
	return run.Then(job)
}

/*
JobElse just returns the job unmodified. Else is used for expressiveness
and can be omitted.

Summary:
  - Run jobs in goroutines : N/A
  - Wait all jobs finish   : N/A
  - Run order              : N/A

Diagram:

	----[JOB]--->
*/
func (tl *TaskList[Pipe]) JobElse(job Job) Job {
	return run.Else(job)
}

/*
JobWait waits until the condition is met. The function falls into sleep with the
duration given between condition checks. The function does not run any job
actually and just repeatedly checks predicate's return value. When the predicate
returns true the function finishes.

Summary:
  - Run jobs in goroutines : N/A
  - Wait all jobs finish   : N/A
  - Run order              : N/A

Diagram:

	                  NO
	  +------(SLEEP)------+
	  |                   |
	  V                   | YES
	----(CONDITION MET?)--+----->
*/
func (tl *TaskList[Pipe]) JobWait(predicate JobPredicate, sleep time.Duration) Job {
	return run.Wait(predicate, sleep)
}

/*
JobWhile repeats running the job while the condition is met.

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	                  YES
	  +-------[JOB]<------+
	  |                   |
	  V                   | NO
	----(CONDITION MET?)--+---->
*/
func (tl *TaskList[Pipe]) JobWhile(predicate JobPredicate, job Job) Job {
	return run.While(predicate, job)
}

/*
JobParallel runs jobs in their own goroutines and waits until all of them finish.

Summary:
  - Run jobs in goroutines : YES
  - Wait all jobs finish   : YES
  - Run order              : PARALLEL

Diagram:

	  +-->[JOB_1]--+
	  |            |
	--+-->  ..   --+-->
	  |            |
	  +-->[JOB_N]--+
*/
func (tl *TaskList[Pipe]) JobParallel(jobs ...Job) Job {
	return run.Parallel(jobs...)
}

/*
JobSequence runs jobs sequentially, one by one.

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SEQUENCE

Diagram:

	-->[JOB_1]-...->[JOB_N]-->
*/
func (tl *TaskList[Pipe]) JobSequence(jobs ...Job) Job {
	return run.Sequence(jobs...)
}

/*
JobRepeat repeats running the job for N times.

Summary:
  - Run jobs in goroutines : NO
  - Wait all jobs finish   : YES
  - Run order              : SINGLE

Diagram:

	                        NO
	  +-----------[JOB]<---------+
	  |                          |
	  V                          | YES
	----(ITERATED COUNT TIMES?)--+---->
*/
func (tl *TaskList[Pipe]) JobRepeat(job Job, times int) Job {
	return run.Repeat(times, job)
}

func (tl *TaskList[Pipe]) JobWaitForTerminator() Job {
	return tl.CreateBasicJob(func() error {
		if !tl.Plumber.Terminator.Enabled {
			return fmt.Errorf("Terminator is not enabled.")
		}

		tl.Log.Traceln("Waiting for the terminator signal...")

		ch := make(chan bool, 1)
		tl.Plumber.Terminator.Terminated.Register(ch)
		defer tl.Plumber.Terminator.Terminated.Unregister(ch)

		<-ch

		return nil
	})
}

// PredicateAnd returns a predicate which chains multiple predicates into a condition
// with AND logic. The result predicate finishes calculation of
// the condition as fast as the result is known. The function panics if
// the number of predicates is less than 2.
//
// The result predicate tests the condition as follows.
//
//	[PRED_1] AND ... AND [PRED_N]
func (tl *TaskList[Pipe]) PredicateAnd(predicates ...JobPredicate) JobPredicate {
	return pred.And(predicates...)
}

// PredicateOr returns a predicate which chains multiple predicates into a condition
// with OR logic. The result predicate finishes calculation of
// the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//
//	[PRED_1] OR ... OR [PRED_N]
func (tl *TaskList[Pipe]) PredicateOr(predicates ...JobPredicate) JobPredicate {
	return pred.Or(predicates...)
}

// PredicateNot returns the negated value of the predicate.
//
// The result predicate tests the condition as follows.
//
//	NOT [PRED]
func (tl *TaskList[Pipe]) PredicateNot(predicate JobPredicate) JobPredicate {
	return pred.Not(predicate)
}

// Xor returns a predicate which chains multiple predicates into a condition
// with XOR logic. The result predicate finishes calculation of
// the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//
//	(([PRED_1] XOR [PRED_2]) ... XOR [PRED_N])
func (tl *TaskList[Pipe]) PredicateXor(predicates ...JobPredicate) JobPredicate {
	return pred.Xor(predicates...)
}

// GuardTimeout protects the job from taking too much time on execution.
// The job is run in it's own goroutine while the current goroutine waits
// until the job finished or time went out or the flow is finished.
func (tl *TaskList[Pipe]) GuardTimeout(job Job, timeout time.Duration) Job {
	return guard.Timeout(guard.ConstTimeout(timeout), nil, job)
}

// OnTimeout protects the job from taking too much time on execution.
// In addition it takes TimeoutTrigger func (t *TaskList[Pipe])  which called if time is out.
// The job is run in it's own goroutine while the current goroutine waits
// until the job finished or time went out or the flow is finished.
func (tl *TaskList[Pipe]) GuardOnTimeout(
	job Job,
	fn GuardHandlerFn[Pipe],
	timeout time.Duration,
) Job {
	return guard.OnTimeout(
		guard.ConstTimeout(timeout),
		nil,
		job,
		func(_ floc.Context, _ floc.Control, _ interface{}) {
			fn(tl)
		},
	)
}

// Panic protects the job from falling into panic. On panic the flow will
// be canceled with the ErrPanic result. Guarding the job from falling into
// panic is effective only if the job runs in the current goroutine.
func (tl *TaskList[Pipe]) GuardPanic(job Job) Job {
	return guard.Panic(
		job,
	)
}

func (tl *TaskList[Pipe]) GuardIgnorePanic(job Job) Job {
	return guard.IgnorePanic(
		job,
	)
}

// OnPanic protects the job from falling into panic. In addition it
// takes PanicTrigger func which is called in case of panic. Guarding the job
// from falling into panic is effective only if the job runs in the current
// goroutine.
func (tl *TaskList[Pipe]) GuardOnPanic(job Job, fn GuardHandlerFn[Pipe]) Job {
	return guard.OnPanic(
		job,
		func(_ floc.Context, _ floc.Control, _ interface{}) {
			fn(tl)
		},
	)
}

// Resume resumes execution of the flow possibly finished by the job.
// If the mask is empty execution will be resumed regardless the reason
// it was finished. Otherwise execution will be resumed if the reason
// it finished with is masked.
func (tl *TaskList[Pipe]) GuardResume(job Job, mask Result) Job {
	return guard.Resume(tl.NewResultMask(mask), job)
}

// Always run this job!
func (tl *TaskList[Pipe]) GuardAlways(job Job) Job {
	return guard.Resume(tl.NewResultMask(TASK_ANY), job)
}

// NewResultMask constructs new instance from the mask given.
func (tl *TaskList[Pipe]) NewResultMask(mask Result) ResultMask {
	return floc.NewResultMask(mask)
}

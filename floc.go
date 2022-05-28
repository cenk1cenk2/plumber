package plumber

import (
	"time"

	"github.com/workanator/go-floc/v3"
	"github.com/workanator/go-floc/v3/guard"
	"github.com/workanator/go-floc/v3/pred"
	"github.com/workanator/go-floc/v3/run"
)

type (
	taskListPredicateFn[Pipe TaskListData, Ctx TaskListData] func(*TaskList[Pipe, Ctx]) bool
	guardHandlerFn[Pipe TaskListData, Ctx TaskListData]      func(*TaskList[Pipe, Ctx])
)

// TaskList.Predicate Creates a new floc predicate out of the given conditions.
func (t *TaskList[Pipe, Ctx]) Predicate(fn taskListPredicateFn[Pipe, Ctx]) floc.Predicate {
	return func(ctx floc.Context) bool {
		return fn(t)
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
func (t *TaskList[Pipe, Ctx]) JobBackground(job floc.Job) floc.Job {
	return run.Background(job)
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
func (t *TaskList[Pipe, Ctx]) JobLoop(job floc.Job) floc.Job {
	return run.Loop(job)
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
func (t *TaskList[Pipe, Ctx]) JobDelay(job floc.Job, delay time.Duration) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobIf(predicate floc.Predicate, job floc.Job) floc.Job {
	return run.If(predicate, job)
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
func (t *TaskList[Pipe, Ctx]) JobIfNot(predicate floc.Predicate, jobs ...floc.Job) floc.Job {
	return run.IfNot(predicate, jobs...)
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
func (t *TaskList[Pipe, Ctx]) JobElse(job floc.Job) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobWait(predicate floc.Predicate, sleep time.Duration) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobWhile(predicate floc.Predicate, job floc.Job) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobParallel(jobs ...floc.Job) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobSequence(jobs ...floc.Job) floc.Job {
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
func (t *TaskList[Pipe, Ctx]) JobRepeat(job floc.Job, times int) floc.Job {
	return run.Repeat(times, job)
}

// PredicateAnd returns a predicate which chains multiple predicates into a condition
// with AND logic. The result predicate finishes calculation of
// the condition as fast as the result is known. The function panics if
// the number of predicates is less than 2.
//
// The result predicate tests the condition as follows.
//   [PRED_1] AND ... AND [PRED_N]
func (t *TaskList[Pipe, Ctx]) PredicateAnd(predicates ...floc.Predicate) floc.Predicate {
	return pred.And(predicates...)
}

// PredicateOr returns a predicate which chains multiple predicates into a condition
// with OR logic. The result predicate finishes calculation of
// the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//   [PRED_1] OR ... OR [PRED_N]
func (t *TaskList[Pipe, Ctx]) PredicateOr(predicates ...floc.Predicate) floc.Predicate {
	return pred.Or(predicates...)
}

// PredicateNot returns the negated value of the predicate.
//
// The result predicate tests the condition as follows.
//   NOT [PRED]
func (t *TaskList[Pipe, Ctx]) PredicateNot(predicate floc.Predicate) floc.Predicate {
	return pred.Not(predicate)
}

// Xor returns a predicate which chains multiple predicates into a condition
// with XOR logic. The result predicate finishes calculation of
// the condition as fast as the result is known.
//
// The result predicate tests the condition as follows.
//   (([PRED_1] XOR [PRED_2]) ... XOR [PRED_N])
func (t *TaskList[Pipe, Ctx]) PredicateXor(predicates ...floc.Predicate) floc.Predicate {
	return pred.Xor(predicates...)
}

// GuardTimeout protects the job from taking too much time on execution.
// The job is run in it's own goroutine while the current goroutine waits
// until the job finished or time went out or the flow is finished.
func (t *TaskList[Pipe, Ctx]) GuardTimeout(job floc.Job, timeout time.Duration) floc.Job {
	return guard.Timeout(guard.ConstTimeout(timeout), nil, job)
}

// OnTimeout protects the job from taking too much time on execution.
// In addition it takes TimeoutTrigger func (t *TaskList[Pipe, Ctx])  which called if time is out.
// The job is run in it's own goroutine while the current goroutine waits
// until the job finished or time went out or the flow is finished.
func (t *TaskList[Pipe, Ctx]) GuardOnTimeout(job floc.Job, fn guardHandlerFn[Pipe, Ctx], timeout time.Duration) floc.Job {
	return guard.OnTimeout(
		guard.ConstTimeout(timeout),
		nil,
		job,
		func(ctx floc.Context, ctrl floc.Control, id interface{}) {
			fn(t)
		},
	)
}

// Panic protects the job from falling into panic. On panic the flow will
// be canceled with the ErrPanic result. Guarding the job from falling into
// panic is effective only if the job runs in the current goroutine.
func (t *TaskList[Pipe, Ctx]) GuardPanic(job floc.Job) floc.Job {
	return guard.Panic(
		job,
	)
}

func (t *TaskList[Pipe, Ctx]) GuardIgnorePanic(job floc.Job) floc.Job {
	return guard.IgnorePanic(
		job,
	)
}

// OnPanic protects the job from falling into panic. In addition it
// takes PanicTrigger func which is called in case of panic. Guarding the job
// from falling into panic is effective only if the job runs in the current
// goroutine.
func (t *TaskList[Pipe, Ctx]) GuardOnPanic(job floc.Job, fn guardHandlerFn[Pipe, Ctx]) floc.Job {
	return guard.OnPanic(
		job,
		func(ctx floc.Context, ctrl floc.Control, id interface{}) {
			fn(t)
		},
	)
}

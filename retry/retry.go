/*
Package retry provides a simple way to retry operations that can
fail, using exponential backoff and jittering between attempts.

	var errHi = errors.New("hi")
	var errBye = errors.New("bye")

	func shouldRetry(err error) bool {
		if err == errBye {
			return false
		}
		return true
	}

	func funcToRetry() error {

		// We use a random number here to simulate
		// different conditions. Purely for demonstration.
		f := rand.Float64()

		// This error will cause Do to stop
		// early as shouldRetry returns false
		// when it receives it.
		if f < 0.2 {
			return errBye
		}

		// Returning nil will always cause Do to
		// stop retrying as it indicates the
		// operation was completed successfully.
		if f < 0.4 {
			return nil
		}

		// Since shouldRetry doesn't test for
		// other errors, errHi will not cause
		// Do to stop retrying.
		return errHi
	}

	func main() {

		d, err := retry.New(shouldRetry, retry.Options{
			Attempts:    5,
			Base:        time.Millisecond * 50,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    2,
			Jitter:      0.5,
		})
		if err != nil {
			log.Fatalln(err)
		}

		rand.Seed(time.Now().UnixNano())

		errs, err := d.Do(funcToRetry)
		for i, err := range errs {
			fmt.Printf("err on attempt %d: %s\n", i+1, err.Error())
		}
		if err != nil {
			fmt.Printf("%s after attempt %d\n", err.Error(), len(errs))
			return
		}
		fmt.Printf("succeeded on attempt %d\n", len(errs)+1)
	}

*/
package retry

import (
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"
)

/*
	ErrMaxAttempts is returned from Do when it could not complete
	its operation and has tried the maximum allowed times.
*/
var ErrMaxAttempts = errors.New("reached maximum attempts")

/*
	ErrCancelled is returned from Do when the operation it is
	attempting returns an error indicating that no further
	attempts should occur. See Retry for more information.
*/
var ErrCancelled = errors.New("further retries cancelled")

/*
	ErrTimeout is returned from Do when the total elapsed time
	attempting the operation exceeds the maximum alloted wait
	time specified by .MaxWait in Options.
*/
var ErrTimeout = errors.New("couldn't complete operation in time")

/*
	errNoFunc is returned by Do when fn is nil - it's a global
	to make testing easier.
*/
var errNoFunc = errors.New("fn is nil")

/*
	Retry is a callback that receives errors returned by the fn parameter
	of Do. Retry can test err for particular errors and return a bool
	indicating whether to continue trying the operation or abort. Retry
	will only be called if there is an error - err is never nil.
*/
type Retry func(err error) (tryAgain bool)

type Options struct {
	/*
		Attempts is a value of 1 or greater that determines the maximum
		number of times an operation will be retried. It is possible for
		for this number of attempts to never be tried either due to the
		successful execution of the operation or because the Retry
		supplied to Do indicates no further attempts should occur.
	*/
	Attempts int

	/*
		Base is a value greater than 0 that determines the initial delay
		before retrying an operation.
	*/
	Base time.Duration

	/*
		MaxInterval is a value greater than or equal to Base that determines
		the longest possible time Do will wait between calls.
	*/
	MaxInterval time.Duration

	/*
		MaxWait is a value greater than or equal to Base that determines the
		maximum time Do will spend trying to successfully execute its operation.
	*/
	MaxWait time.Duration

	/*
		Exponent is a value greater than 1 that determines the growth rate of
		the interval between retries. For example an Exponent of 2 would double
		the delay each time meaning a Base of 20 milliseconds on the first
		attempt would become 40 on the second, 80 on the third, and so on.
	*/
	Exponent float64

	/*
	   Jitter is a value between 0 and 1 which is used to determine
	   how much randomness affects intervals between retries.

	   For an interval of 200:

	       0    // Interval remains 200
	       0.75 // Interval is a random number between 150 and 200
	       0.5  // Interval is a random number between 100 and 200
	       0.25 // Interval is a random number between 50 and 200
	       1    // Interval is a random number between 0 and 200

	   An error is returned by New if Jitter is less than 0 or greater
	   than 1.
	*/
	Jitter float64
}

/*
	Exported only for documentation purposes. Use New to initialise a
	new Doer.
*/
type Doer struct {
	base        float64
	maxInterval float64
	exponent    float64
	jitter      float64
	attempts    int
	maxWait     time.Duration
	seed        int64
	seedMu      sync.Mutex
	retry       Retry
}

/*
	New returns a Doer with Options o. The retry parameter is optional -
	if it is nil Do will always retry fn when it fails, up to o.Attempts.
	See Retry for more information.

	New returns an error if the fields in o contain invalid values. See
	Options for information on what the valid ranges are for each field.
*/
func New(retry Retry, o Options) (*Doer, error) {

	if o.Attempts < 1 {
		return nil, fmt.Errorf("expected an .Attempts value greater than 0, got %d", o.Attempts)
	}

	if o.Base <= 0 {
		return nil, fmt.Errorf("expected .Base to be greater than 0, got %d", o.Base)
	}

	if o.MaxInterval < o.Base {
		return nil, fmt.Errorf(
			"expected .MaxInterval to be greater than .Base, got .MaxInterval %d and .Base %d",
			o.MaxInterval,
			o.Base,
		)
	}

	if o.MaxWait < o.Base {
		return nil, fmt.Errorf(
			"expected .MaxWait to be greater than .Base, got .MaxWait %d and .Base %d",
			o.MaxWait,
			o.Base,
		)
	}

	if o.Exponent < 1 {
		return nil, fmt.Errorf(
			"expected .Exponent to be greater than or equal to 1, got %.2f", o.Exponent)
	}

	if o.Jitter < 0 || o.Jitter > 1 {
		return nil, fmt.Errorf("expected a .Jitter value between 0 and 1, got %.2f", o.Jitter)
	}

	return &Doer{
		seed:        time.Now().UnixNano(),
		seedMu:      sync.Mutex{},
		attempts:    o.Attempts,
		base:        float64(o.Base),
		maxInterval: float64(o.MaxInterval),
		maxWait:     o.MaxWait,
		exponent:    o.Exponent,
		jitter:      o.Jitter,
		retry:       retry,
	}, nil
}

/*
	Operation is a function passed to a Doer's Do method. It will be executed
	repeatedly until it returns nil or until it returns an error that Retry
	decides is permanent.
*/
type Operation func() error

/*
	Do calls fn repeatedly until it succeeds, or until fn returns an error
	that the Retry passed to New decides is permanent, or until fn has been
	called up to the maximum number of attempts specified in the Options
	passed to New.

	Do returns a slice of errors from calls to fn in the order they occured,
	and an overall error from Do.

	The number of attempts for a failed operation (i.e., when err is not nil)
	is always len(errs) while the number of attempts for a successful operation
	(where err is nil) is always len(errs)+1.
*/
func (d Doer) Do(fn Operation) (errs []error, err error) {

	if fn == nil {
		return errs, errNoFunc
	}

	/*
		We avoid using the current time as a seed because multiple
		goroutines may be calling fn simultaneously. If they have
		the same seed their jitter will not distribute those calls,
		which is the purpose of jitter to begin with.
	*/
	d.seedMu.Lock()
	d.seed++
	d.seedMu.Unlock()
	r := rand.New(rand.NewSource(d.seed))

	var total time.Duration

	for attempt := 0; attempt < d.attempts; attempt++ {

		err := fn()
		if err == nil {
			return errs, nil
		}
		errs = append(errs, err)

		if d.retry != nil && !d.retry(err) {
			return errs, ErrCancelled
		}

		sleep := d.base * math.Pow(d.exponent, float64(attempt))

		sleep = math.Min(d.maxInterval, sleep)

		sleep *= (1 - (r.Float64() * d.jitter))

		total += time.Duration(sleep)
		if total > d.maxWait {
			return errs, ErrTimeout
		}

		time.Sleep(time.Nanosecond * time.Duration(sleep))
	}

	return errs, ErrMaxAttempts
}

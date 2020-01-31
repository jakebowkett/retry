/*
Package retry provides a simple way to retry operations that can
fail, using exponential backoff and jittering between attempts.

	func main() {

		r, err := retry.New(shouldRetry, retry.Options{
			Retries:     3,
			Base:        time.Millisecond * 50,
			MaxInterval: time.Second * 1,
			MaxWait:     time.Second * 2,
			Exponent:    2,
			Jitter:      0.5,
		})
		if err != nil {
			log.Fatalln(err)
		}

		// This will fail after 3 attempts.
		_, err = r.Try(func() error {
			return errors.New("error")
		})
		log.Println(err.Error())

		// This will fail after the first attempt
		// because shouldRetry signals we should
		// abort upon receiving errPermanent.
		_, err = r.Try(func() error {
			return errPermanent
		})

		// This will succeed on the third attempt.
		attempt := 0
		_, _ = r.Try(func() error {
			attempt++
			if attempt == 3 {
				return nil
			}
			return errors.New("error")
		})
		log.Printf("operation succeeded on attempt %d", attempt)
	}

	var errPermanent = errors.New("permanent")

	func shouldRetry(err error) bool {

		// We should not retry the operation
		// after receiving an errPermanent.
		if err == errPermanent {
			return false
		}

		// For all other errors we retry.
		return true
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
	ErrMaxAttempts is returned from Try when it could not complete
	its operation and has tried the maximum allowed times.
*/
var ErrMaxAttempts = errors.New("reached maximum attempts")

/*
	ErrCancelled is returned from Try when the operation it is
	attempting returns an error indicating that no further
	attempts should occur. See Retry for more information.
*/
var ErrCancelled = errors.New("further retries cancelled")

/*
	ErrTimeout is returned from Try when the total elapsed time
	attempting the operation exceeds the maximum alloted wait
	time specified by .MaxWait in Options.
*/
var ErrTimeout = errors.New("couldn't complete operation in time")

/*
	errNoFunc is returned by Try when fn is nil - it's a global
	to make testing easier.
*/
var errNoFunc = errors.New("fn is nil")

/*
	Retry is a callback that receives errors returned by the fn parameter
	of Try. Retry can test err for particular errors and return a bool
	indicating whether to continue trying the operation or abort. Retry
	will only be called if there is an error therefore err is never nil.
*/
type Retry = func(err error) (tryAgain bool)

type Options struct {
	/*
		Retries is a value of 0 or greater that determines the maximum
		number of times an operation will be retried after the initial
		attempt. It is possible this number of retries will never be
		reached either due to the successful execution of the operation
		or because the Retry supplied to Try indicates no further attempts
		should occur.
	*/
	Retries int

	/*
		Base determines the initial delay before retrying an operation.
	*/
	Base time.Duration

	/*
		MaxInterval is a value greater than or equal to Base that determines
		the longest possible time Try will wait between calls.
	*/
	MaxInterval time.Duration

	/*
		MaxWait is a value greater than or equal to Base that determines the
		maximum time Try will spend trying to successfully execute its operation.
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
	new Tryer.
*/
type Tryer struct {
	base        float64
	maxInterval float64
	exponent    float64
	jitter      float64
	retries     int
	maxWait     time.Duration
	seed        int64
	seedMu      sync.Mutex
	retry       Retry
}

/*
	New returns a Tryer with Options o. The retry parameter is optional -
	if it is nil Try will always retry fn when it fails, up to o.Retries.
	See Retry for more information.

	New returns an error if the fields in o contain invalid values. See
	Options for information on what the valid ranges are for each field.
*/
func New(retry Retry, o Options) (*Tryer, error) {

	if o.Exponent < 1 {
		return nil, fmt.Errorf(
			"expected .Exponent to be greater than or equal to 1, got %.2f", o.Exponent)
	}

	if o.Jitter < 0 || o.Jitter > 1 {
		return nil, fmt.Errorf("expected a .Jitter value between 0 and 1, got %.2f", o.Jitter)
	}

	return &Tryer{
		seed:        time.Now().UnixNano(),
		seedMu:      sync.Mutex{},
		retries:     o.Retries,
		base:        float64(o.Base),
		maxInterval: float64(o.MaxInterval),
		maxWait:     o.MaxWait,
		exponent:    o.Exponent,
		jitter:      o.Jitter,
		retry:       retry,
	}, nil
}

/*
	Operation is a function passed to a Tryer's Try method. It will be executed
	repeatedly until it returns nil or until it returns an error that Retry
	decides is permanent.
*/
type Operation = func() error

/*
	Try calls fn repeatedly until it succeeds, or until fn returns an error
	that the Retry passed to New decides is permanent, or until fn has been
	called up to the maximum number of attempts specified in the Options
	passed to New.

	Try returns a slice of errors from calls to fn in the order they occured,
	and an overall error from Try.

	The number of attempts for a failed operation (i.e., when err is not nil)
	is always len(errs) while the number of attempts for a successful operation
	(where err is nil) is always len(errs)+1.
*/
func (t Tryer) Try(fn Operation) (errs []error, err error) {

	if fn == nil {
		return errs, errNoFunc
	}

	/*
		We avoid using the current time as a seed because multiple
		goroutines may be calling fn simultaneously. If they have
		the same seed their jitter will not distribute those calls,
		which is the purpose of jitter to begin with.
	*/
	t.seedMu.Lock()
	t.seed++
	t.seedMu.Unlock()
	r := rand.New(rand.NewSource(t.seed))

	var total time.Duration

	for attempt := 0; attempt <= t.retries; attempt++ {

		err := fn()
		if err == nil {
			return errs, nil
		}
		errs = append(errs, err)

		if t.retry != nil && !t.retry(err) {
			return errs, ErrCancelled
		}

		sleep := t.base * math.Pow(t.exponent, float64(attempt))

		sleep = math.Min(t.maxInterval, sleep)

		sleep *= (1 - (r.Float64() * t.jitter))

		total += time.Duration(sleep)
		if total > t.maxWait {
			return errs, ErrTimeout
		}

		time.Sleep(time.Nanosecond * time.Duration(sleep))
	}

	return errs, ErrMaxAttempts
}

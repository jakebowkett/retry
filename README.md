# go-retry

Package retry provides a simple way to retry operations that can
fail, using exponential backoff and jittering between attempts.

```Go
func main() {

    d, err := retry.New(shouldRetry, retry.Options{
        Attempts:    3,
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
    _, err = d.Do(func() error {
        return errors.New("error")
    })
    log.Println(err.Error())

    // This will fail after the first attempt
    // because shouldRetry signals we should
    // abort upon receiving errPermanent.
    _, err = d.Do(func() error {
        return errPermanent
    })

    // This will succeed on the third attempt.
    attempt := 0
    _, _ = d.Do(func() error {
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
```

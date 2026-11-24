package awshealth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/health"
)

func immediateRetryer(attempts int) func() aws.Retryer {
	return func() aws.Retryer {
		return retry.NewStandard(func(o *retry.StandardOptions) {
			o.MaxAttempts = attempts
			o.Backoff = retry.BackoffDelayerFunc(func(int, error) (time.Duration, error) { return 0, nil })
		})
	}
}

func pacingConfig(transport healthHTTPClient) aws.Config {
	return aws.Config{Region: "us-east-1", Credentials: credentials.NewStaticCredentialsProvider("test", "secret", "token"), Retryer: immediateRetryer(4), HTTPClient: transport}
}

func sdkResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"application/x-amz-json-1.1"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}
}

func TestHealthPacingSharesEveryOperationPageAndRetry(t *testing.T) {
	const interval = 10 * time.Millisecond
	var mu sync.Mutex
	counts := make(map[string]int)
	var attempts []time.Time
	cfg := pacingConfig(func(request *http.Request) (*http.Response, error) {
		op := strings.TrimPrefix(request.Header.Get("X-Amz-Target"), "AWSHealth_20160804.")
		mu.Lock()
		counts[op]++
		n := counts[op]
		attempts = append(attempts, time.Now())
		mu.Unlock()
		if n <= 2 {
			return sdkResponse(request, 400, `{"__type":"ThrottlingException","message":"Rate exceeded"}`), nil
		}
		body := ""
		switch op {
		case "DescribeEventsForOrganization":
			body = `{"events":[]}`
			if n == 3 {
				body = `{"events":[],"nextToken":"another-page"}`
			}
		case "DescribeAffectedAccountsForOrganization":
			body = `{"affectedAccounts":[]}`
		case "DescribeEventDetailsForOrganization":
			body = `{"successfulSet":[{"awsAccountId":"111111111111","event":{"arn":"event"},"eventDescription":{"latestDescription":"Upgrade"}}]}`
		case "DescribeAffectedEntitiesForOrganization":
			body = `{"entities":[]}`
		default:
			return nil, errors.New("unexpected Health operation")
		}
		return sdkResponse(request, 200, body), nil
	})
	client := NewClient(cfg, func(o *ClientOptions) { o.RequestInterval = interval })
	calls := []func() error{
		func() error { _, err := client.ListUpcomingEvents(context.Background(), testNow); return err },
		func() error { _, err := client.AffectedAccounts(context.Background(), "event"); return err },
		func() error { _, err := client.EventDetails(context.Background(), "event", testAccount); return err },
		func() error {
			_, err := client.AffectedResources(context.Background(), "event", testAccount)
			return err
		},
	}
	var wg sync.WaitGroup
	for _, call := range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := call(); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if len(attempts) != 13 || counts["DescribeEventsForOrganization"] != 4 {
		t.Fatal(counts, len(attempts))
	}
	// Allow some signing/scheduling overhead between admission and the HTTP hook.
	// Without a shared per-attempt gate the retries/page calls form a short burst.
	if elapsed := attempts[len(attempts)-1].Sub(attempts[0]); elapsed < 12*interval-interval/2 {
		t.Fatalf("13 requests took only %s; pacing was bypassed", elapsed)
	}
}

func TestHealthPacingAfterCredentialRefresh(t *testing.T) {
	const workers = 4
	const interval = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	refreshDone := make(chan struct{})
	var finishRefresh sync.Once
	unblockRefresh := func() { finishRefresh.Do(func() { close(refreshDone) }) }
	defer unblockRefresh()
	var retrieves atomic.Int32
	cache := aws.NewCredentialsCache(aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
		if retrieves.Add(1) > 1 {
			select {
			case <-refreshDone:
			case <-ctx.Done():
				return aws.Credentials{}, ctx.Err()
			}
		}
		return aws.Credentials{
			AccessKeyID: "test", SecretAccessKey: "secret", SessionToken: "token",
			CanExpire: true, Expires: time.Now().Add(time.Hour),
		}, nil
	}))
	if _, err := cache.Retrieve(ctx); err != nil {
		t.Fatal(err)
	}
	cache.Invalidate()

	var mu sync.Mutex
	var attempts []time.Time
	cfg := pacingConfig(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		attempts = append(attempts, time.Now())
		mu.Unlock()
		return sdkResponse(request, 200, `{"affectedAccounts":[]}`), nil
	})
	waiting := make(chan struct{}, workers)
	cfg.Credentials = aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
		select {
		case waiting <- struct{}{}:
		case <-ctx.Done():
			return aws.Credentials{}, ctx.Err()
		}
		return cache.Retrieve(ctx)
	})
	client := NewClient(cfg, func(o *ClientOptions) { o.RequestInterval = interval })
	errs := make(chan error, workers)
	for range workers {
		go func() {
			_, err := client.AffectedAccounts(ctx, "event")
			errs <- err
		}()
	}
	// Let every worker reach the shared refresh before completing it. Pacing
	// before credential retrieval would let all requests send together afterward.
	for range workers {
		select {
		case <-waiting:
		case <-ctx.Done():
			t.Fatal("waiting for credential refresh:", ctx.Err())
		}
	}
	unblockRefresh()
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if retrieves.Load() != 2 || len(attempts) != workers {
		t.Fatalf("credential retrievals=%d, HTTP attempts=%d", retrieves.Load(), len(attempts))
	}
	// Allow signing/scheduling overhead between pacing and the HTTP hook.
	for i := 1; i < len(attempts); i++ {
		if gap := attempts[i].Sub(attempts[i-1]); gap < interval/2 {
			t.Fatalf("HTTP attempts %d and %d were only %s apart after credential refresh", i, i+1, gap)
		}
	}
}

func TestHealthRetriesItemLevelThrottlesAndRetainsOtherErrors(t *testing.T) {
	for _, operation := range []string{"details", "resources"} {
		for _, code := range []string{"ThrottlingException", "AccessDeniedException"} {
			t.Run(operation+"/"+code, func(t *testing.T) {
				attempts := 0
				cfg := pacingConfig(func(request *http.Request) (*http.Response, error) {
					attempts++
					body := `{"failedSet":[{"awsAccountId":"111111111111","eventArn":"event","errorName":"` + code + `","errorMessage":"test failure"}]}`
					if attempts > 1 {
						body = `{"entities":[]}`
						if operation == "details" {
							body = `{"successfulSet":[{"awsAccountId":"111111111111","event":{"arn":"event"},"eventDescription":{"latestDescription":"Upgrade"}}]}`
						}
					}
					return sdkResponse(request, 200, body), nil
				})
				client := NewClient(cfg, func(o *ClientOptions) { o.RequestInterval = time.Millisecond })
				var err error
				if operation == "details" {
					_, err = client.EventDetails(context.Background(), "event", testAccount)
				} else {
					_, err = client.AffectedResources(context.Background(), "event", testAccount)
				}
				if code == "ThrottlingException" {
					if err != nil || attempts != 2 {
						t.Fatalf("attempts=%d err=%v", attempts, err)
					}
				} else if err == nil || attempts != 1 {
					t.Fatalf("permission error retried or hidden: attempts=%d err=%v", attempts, err)
				}
			})
		}
	}
}

func TestHealthRetryDefaultsAndExhaustion(t *testing.T) {
	client := NewClient(aws.Config{})
	options := client.api.(*health.Client).Options()
	if options.Retryer.MaxAttempts() != 6 {
		t.Fatal(options.Retryer.MaxAttempts())
	}
	if options.RetryMode != aws.RetryModeAdaptive {
		t.Fatalf("retry mode=%s", options.RetryMode)
	}
	client = NewClient(aws.Config{RetryMode: aws.RetryModeStandard, RetryMaxAttempts: 9})
	if client.api.(*health.Client).Options().Retryer.MaxAttempts() != 9 {
		t.Fatal("explicit retry settings lost")
	}
	attempts := 0
	cfg := pacingConfig(func(request *http.Request) (*http.Response, error) {
		attempts++
		return sdkResponse(request, 400, `{"__type":"ThrottlingException","message":"Rate exceeded"}`), nil
	})
	cfg.Retryer = immediateRetryer(2)
	client = NewClient(cfg, func(o *ClientOptions) { o.RequestInterval = time.Millisecond })
	accounts, err := client.AffectedAccounts(context.Background(), "event")
	if err == nil || accounts != nil || attempts != 2 || !strings.Contains(err.Error(), "ThrottlingException") {
		t.Fatal(accounts, attempts, err)
	}
}

func TestHealthPacingCancellationDoesNotReserveSlots(t *testing.T) {
	pacer := &requestPacer{interval: time.Hour}
	if err := pacer.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := pacer.next
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	if err := pacer.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !pacer.next.Equal(next) {
		t.Fatal("canceled request left a reservation")
	}
	if _, err := FetchUpcoming(context.Background(), Query{RequestInterval: -time.Second}); err == nil {
		t.Fatal("negative request interval accepted")
	}
}

func TestHealthSDKCancelsQueuedRequestWithoutSending(t *testing.T) {
	attempts := 0
	client := NewClient(pacingConfig(func(request *http.Request) (*http.Response, error) {
		attempts++
		return sdkResponse(request, 200, `{"affectedAccounts":[]}`), nil
	}), func(o *ClientOptions) { o.RequestInterval = time.Hour })
	if _, err := client.AffectedAccounts(context.Background(), "event"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(time.Millisecond, cancel)
	defer timer.Stop()
	if _, err := client.AffectedAccounts(ctx, "event"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("queued canceled request made an HTTP attempt: %d", attempts)
	}
}

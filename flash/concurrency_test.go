package flash_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/goliatone/go-router"
	"github.com/goliatone/go-router/flash"
	"github.com/stretchr/testify/mock"
)

func TestFlash_ConcurrentAccess_DoesNotRace(t *testing.T) {
	// Use a non-zero Expires to avoid MockContext treating the cookie as expired.
	f := flash.New(flash.Config{Name: "race-flash", Expires: time.Now().Add(time.Hour)})

	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	errCh := make(chan error, goroutines*iterations)

	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				ctx := router.NewMockContext()
				ctx.On("Cookie", mock.Anything).Return(nil)
				ctx.On("Locals", mock.Anything, mock.Anything).Return(nil)

				wantID := fmt.Sprintf("%d-%d", g, i)
				f.WithData(ctx, router.ViewContext{"id": wantID})
				got := f.Get(ctx)

				id, _ := got["id"].(string)
				if id != wantID {
					errCh <- fmt.Errorf("got id %q, want %q", id, wantID)
					return
				}
			}
		}(g)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Fatal(err)
	}
}

package app

import (
	"encoding/hex"
	"os"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // zone lookups must not depend on the host's zoneinfo

	"github.com/caarlos0/env/v11"

	"github.com/dirty-go/foundation/errors"
)

// resetForTest clears the package-level Init state now and on cleanup. Tests
// using it touch globals and must not run in parallel.
func resetForTest(t *testing.T) {
	t.Helper()
	reset := func() {
		defApp.Store(nil)
		errDefApp, defAppOnce = nil, sync.Once{}
	}
	reset()
	t.Cleanup(reset)
}

func fixedID() (string, error) { return "id-1", nil }

func isInstanceID(s string) bool {
	_, err := hex.DecodeString(s)
	return len(s) == 32 && err == nil
}

func TestInitFirstCallWins(t *testing.T) {
	resetForTest(t)

	first, err := initApp(&Info{Name: "first", TZLocation: "UTC"}, fixedID)
	if err != nil || first == nil {
		t.Fatalf("Init = (%v, %v), want App, nil", first, err)
	}
	second, err := Init(&Info{Name: "second"})
	if err != nil {
		t.Fatalf("second Init err = %v", err)
	}
	if second != first {
		t.Error("second Init returned a different App")
	}
	if got := second.AppInfo().Name; got != "first" {
		t.Errorf("Name = %q, want %q", got, "first")
	}
	if got := first.InstanceID(); got != "id-1" {
		t.Errorf("InstanceID = %q, want %q", got, "id-1")
	}
	if got := first.AppInfo().Env; got != EnvDefault {
		t.Errorf("Env = %q, want %q", got, EnvDefault)
	}
}

func TestInitStickyError(t *testing.T) {
	wantErr := errors.New("no id")
	calls := 0
	resetForTest(t)
	failing := func() (string, error) { calls++; return "", wantErr }

	for i := range 3 {
		a, err := initApp(nil, failing)
		if a != nil || !errors.Is(err, wantErr) {
			t.Fatalf("call %d: Init = (%v, %v), want (nil, %v)", i, a, err, wantErr)
		}
	}
	if calls != 1 {
		t.Errorf("instance ID source called %d times, want 1", calls)
	}
}

func TestInitPanicKeepsStickyError(t *testing.T) {
	resetForTest(t)
	func() {
		defer func() { _ = recover() }()
		_, _ = initApp(nil, func() (string, error) { panic("boom") })
	}()

	a, err := initApp(nil, func() (string, error) { return "id", nil })
	if a != nil || !errors.Is(err, errInitPanicked) {
		t.Fatalf("Init after a recovered panic = (%v, %v), want (nil, %v)", a, err, errInitPanicked)
	}
}

func TestInfoTZ(t *testing.T) {
	tests := []struct {
		name string
		info Info
		want string
	}{
		{"zero Info", Info{}, "UTC"},
		{"zone without Init", Info{TZLocation: "Asia/Jakarta"}, "Asia/Jakarta"},
		{"invalid zone", Info{TZLocation: "Not/AZone"}, "UTC"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.info.TZ().String(); got != tt.want {
				t.Errorf("TZ() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTZLocationBeforeInit(t *testing.T) {
	resetForTest(t)
	if got := TZLocation(); got != time.UTC {
		t.Errorf("TZLocation() = %v, want UTC", got)
	}
}

func TestTZAgreesWithInit(t *testing.T) {
	resetForTest(t)
	info := Info{TZLocation: "Asia/Jakarta"}
	a, err := initApp(&info, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	want := info.TZ().String()
	if got := a.AppInfo().TZ().String(); got != want {
		t.Errorf("Init TZ = %q, Info.TZ() = %q", got, want)
	}
	if got := TZLocation().String(); got != want {
		t.Errorf("TZLocation() = %q, want %q", got, want)
	}
}

func TestInitInvalidTZFallsBackToLocal(t *testing.T) {
	resetForTest(t)
	a, err := initApp(&Info{TZLocation: "Not/AZone"}, fixedID)
	if err != nil {
		t.Fatal(err)
	}
	if got := a.AppInfo().TZ(); got != time.Local {
		t.Errorf("TZ() = %v, want time.Local", got)
	}
}

func TestInitNilUsesDefaultInfo(t *testing.T) {
	resetForTest(t)
	a, err := Init(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := DefaultInfo()
	want.location = time.UTC
	if got := a.AppInfo(); got != want {
		t.Errorf("AppInfo() = %+v, want %+v", got, want)
	}
	if id := a.InstanceID(); !isInstanceID(id) {
		t.Errorf("InstanceID() = %q, want 32 hex chars", id)
	}
}

func TestInitByEnvParseErrorKeepsOnce(t *testing.T) {
	resetForTest(t)
	// Info's fields are plain strings, so no env value fails to parse;
	// RequiredIfNoDef with APP_URL unset forces the error path instead.
	t.Setenv("APP_URL", "")
	if err := os.Unsetenv("APP_URL"); err != nil {
		t.Fatal(err)
	}
	if a, err := initByEnv(env.Options{Prefix: EnvPrefixDefault, RequiredIfNoDef: true}); err == nil || a != nil {
		t.Fatalf("initByEnv = (%v, %v), want (nil, error)", a, err)
	}
	a, err := initApp(&Info{Name: "after"}, fixedID)
	if err != nil {
		t.Fatalf("Init after env error: %v", err)
	}
	if got := a.AppInfo().Name; got != "after" {
		t.Errorf("Name = %q, want %q (env error consumed the once)", got, "after")
	}
}

func TestInitConcurrent(t *testing.T) {
	resetForTest(t)
	const n = 16
	apps := make([]App, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := initApp(&Info{TZLocation: "UTC"}, fixedID)
			if err != nil {
				t.Error(err)
			}
			apps[i] = a
		}()
	}
	wg.Wait()
	for i, a := range apps {
		if a == nil || a != apps[0] {
			t.Fatalf("apps[%d] = %v, want %v", i, a, apps[0])
		}
	}
}

func TestTZLocationConcurrentWithInit(t *testing.T) {
	resetForTest(t)
	jakarta, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 100 {
				if loc := TZLocation(); loc != time.UTC && loc.String() != jakarta.String() {
					t.Errorf("TZLocation() = %v, want UTC or %v", loc, jakarta)
					return
				}
			}
		}()
	}
	close(start)
	if _, err := initApp(&Info{TZLocation: "Asia/Jakarta"}, fixedID); err != nil {
		t.Error(err)
	}
	wg.Wait()
	if got := TZLocation().String(); got != jakarta.String() {
		t.Errorf("TZLocation() after Init = %q, want %q", got, jakarta)
	}
}

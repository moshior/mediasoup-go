package mediasoup

import (
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var worker *Worker

func init() {
	os.Setenv("DEBUG_COLORS", "false")
	DefaultLevel = WarnLevel
	// WorkerBin = "../mediasoup/worker/out/Release/mediasoup-worker"
	worker = CreateTestWorker()
}

func CreateTestWorker(options ...Option) *Worker {
	defaultOptions := []Option{WithLogLevel("debug"), WithLogTags([]WorkerLogTag{"info"})}
	options = append(defaultOptions, options...)

	worker, err := NewWorker(options...)
	if err != nil {
		panic(err)
	}

	return worker
}

func TestCreateWorker_Succeeds(t *testing.T) {
	worker := CreateTestWorker()
	assert.NotZero(t, worker.Pid())
	assert.False(t, worker.Closed())

	worker.Close()
	assert.True(t, worker.Closed())

	worker = CreateTestWorker(
		WithLogLevel(WorkerLogLevel_Debug),
		WithLogTags([]WorkerLogTag{WorkerLogTag_INFO}),
		WithRtcMinPort(0),
		WithRtcMaxPort(9999),
		WithDtlsCert("testdata/dtls-cert.pem", "testdata/dtls-key.pem"),
		func(o *WorkerSettings) {
			o.AppData = H{"bar": 456}
		},
	)
	assert.NotZero(t, worker.Pid())
	assert.False(t, worker.Closed())
	assert.Equal(t, H{"bar": 456}, worker.AppData())

	worker.Close()
	assert.True(t, worker.Closed())
}

func TestCreateWorker_TypeError(t *testing.T) {
	_, err := NewWorker(WithLogLevel("chicken"))
	assert.IsType(t, TypeError{}, err)

	_, err = NewWorker(WithRtcMinPort(1000), WithRtcMaxPort(999))
	assert.IsType(t, TypeError{}, err)

	_, err = NewWorker(WithDtlsCert("/notfound/cert.pem", "/notfound/priv.pem"))
	assert.IsType(t, TypeError{}, err)
}

func TestWorkerUpdateSettings_Succeeds(t *testing.T) {
	worker := CreateTestWorker()
	err := worker.UpdateSettings(WorkerUpdateableSettings{LogLevel: "debug", LogTags: []WorkerLogTag{"ice"}})
	assert.NoError(t, err)
	worker.Close()
}

func TestWorkerUpdateSettings_TypeError(t *testing.T) {
	worker := CreateTestWorker()
	err := worker.UpdateSettings(WorkerUpdateableSettings{LogLevel: "chicken"})
	assert.IsType(t, TypeError{}, err)
	worker.Close()
}

func TestWorkerUpdateSettings_InvalidStateError(t *testing.T) {
	worker := CreateTestWorker()
	worker.Close()

	err := worker.UpdateSettings(WorkerUpdateableSettings{LogLevel: "error"})
	assert.IsType(t, InvalidStateError{}, err)
}

func TestWorkerDump(t *testing.T) {
	worker := CreateTestWorker()
	defer worker.Close()

	dump, err := worker.Dump()
	assert.NoError(t, err)
	assert.Equal(t, worker.Pid(), dump.Pid)
	assert.Empty(t, dump.RouterIds)
}

func TestWorkerDump_InvalidStateError(t *testing.T) {
	worker := CreateTestWorker()
	worker.Close()

	_, err := worker.Dump()
	assert.Error(t, err)
}

func TestWorkerGetResourceUsage_Succeeds(t *testing.T) {
	worker := CreateTestWorker()
	defer worker.Close()

	_, err := worker.GetResourceUsage()
	assert.NoError(t, err)
}

func TestWorkerClose_Succeeds(t *testing.T) {
	worker := CreateTestWorker(WithLogLevel("warn"))

	onObserverClose := NewMockFunc(t)
	worker.Observer().Once("close", onObserverClose.Fn())

	worker.Close()

	onObserverClose.ExpectCalledTimes(1)
	assert.True(t, worker.Closed())
}

func TestWorkerEmitsDied(t *testing.T) {
	signals := []os.Signal{os.Interrupt, syscall.SIGTERM, os.Kill}

	for _, signal := range signals {

		worker := CreateTestWorker(WithLogLevel("warn"))

		onObserverClose := NewMockFunc(t)
		worker.Observer().Once("close", onObserverClose.Fn())

		process, err := os.FindProcess(worker.Pid())
		assert.NoError(t, err)

		diedCh := make(chan struct{})
		worker.On("died", func() {
			require.Zero(t, len(onObserverClose.results), `observer "close" event emitted before worker "died" event`)
			require.True(t, worker.Closed(), "worker.closed is false")
			close(diedCh)
		})

		process.Signal(signal)

		select {
		case <-diedCh:
		case <-time.NewTimer(time.Second).C:
			t.Fatalf("timeout signal: %s", signal)
		}

		onObserverClose.ExpectCalledTimes(1)
		assert.True(t, worker.Closed())
		assert.True(t, worker.Died())
	}
}

func TestWorkerProcessIgnoreSignals(t *testing.T) {
	// Windows doesn't have some signals such as SIGPIPE, SIGALRM, SIGUSR1, SIGUSR2
	// so we just skip this test in Windows.
	if runtime.GOOS == "windows" {
		return
	}

	worker := CreateTestWorker(WithLogLevel("warn"))

	onObserverDied := NewMockFunc(t)
	worker.On("died", onObserverDied.Fn())

	process, err := os.FindProcess(worker.Pid())
	assert.NoError(t, err)

	process.Signal(syscall.SIGPIPE)
	process.Signal(syscall.SIGHUP)
	process.Signal(syscall.SIGALRM)
	// process.Signal(syscall.SIGUSR1)
	// process.Signal(syscall.SIGUSR2)

	time.Sleep(10 * time.Millisecond)

	onObserverDied.ExpectCalledTimes(0)
	assert.False(t, worker.Closed())
	worker.Close()
}

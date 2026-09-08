package orcraft

import (
	"errors"
	"io"
	"testing"
)

type snapshotTestApp struct {
	data []byte
	err  error
}

func (app *snapshotTestApp) GetData() ([]byte, error) {
	return app.data, app.err
}

func (app *snapshotTestApp) Restore(io.ReadCloser) error {
	return nil
}

type snapshotTestSink struct {
	writeErr   error
	shortWrite bool
	closeErr   error
	cancelErr  error
	closed     bool
	canceled   bool
}

func (sink *snapshotTestSink) ID() string {
	return "test"
}

func (sink *snapshotTestSink) Write(data []byte) (int, error) {
	if sink.writeErr != nil {
		return 0, sink.writeErr
	}
	if sink.shortWrite && len(data) > 0 {
		return len(data) - 1, nil
	}
	return len(data), nil
}

func (sink *snapshotTestSink) Close() error {
	sink.closed = true
	return sink.closeErr
}

func (sink *snapshotTestSink) Cancel() error {
	sink.canceled = true
	return sink.cancelErr
}

func TestFSMSnapshotCancelsSinkBeforeReturningAnError(t *testing.T) {
	tests := []struct {
		name       string
		app        *snapshotTestApp
		writeErr   error
		shortWrite bool
	}{
		{name: "snapshot data", app: &snapshotTestApp{err: errors.New("get data failed")}},
		{name: "snapshot write", app: &snapshotTestApp{data: []byte("state")}, writeErr: errors.New("write failed")},
		{name: "short snapshot write", app: &snapshotTestApp{data: []byte("state")}, shortWrite: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &snapshotTestSink{writeErr: tc.writeErr, shortWrite: tc.shortWrite}
			err := newFsmSnapshot(tc.app).Persist(sink)
			if err == nil {
				t.Fatal("Persist() error = nil")
			}
			if !sink.canceled {
				t.Fatal("Persist() did not cancel the snapshot sink")
			}
			if sink.closed {
				t.Fatal("Persist() closed an unsuccessful snapshot sink")
			}
		})
	}
}

func TestFSMSnapshotClosesSuccessfulSink(t *testing.T) {
	sink := &snapshotTestSink{}
	if err := newFsmSnapshot(&snapshotTestApp{data: []byte("state")}).Persist(sink); err != nil {
		t.Fatalf("Persist() error = %v", err)
	}
	if !sink.closed || sink.canceled {
		t.Fatalf("successful sink closed=%t canceled=%t", sink.closed, sink.canceled)
	}
}

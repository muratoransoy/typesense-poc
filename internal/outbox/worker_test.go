package outbox

import (
	"context"
	"errors"
	"testing"
)

type fakeSearchClient struct {
	upsertPayloads [][]byte
	deletedIDs     []string
	upsertErr      error
	deleteErr      error
}

func (f *fakeSearchClient) UpsertRawJSON(ctx context.Context, rawJSON []byte) error {
	payload := append([]byte(nil), rawJSON...)
	f.upsertPayloads = append(f.upsertPayloads, payload)
	return f.upsertErr
}

func (f *fakeSearchClient) DeleteDocument(ctx context.Context, id string) error {
	f.deletedIDs = append(f.deletedIDs, id)
	return f.deleteErr
}

func TestProcessEventUpsertSendsPayloadToTypesense(t *testing.T) {
	search := &fakeSearchClient{}
	worker := &Worker{search: search}
	event := Event{
		RecordID:      "product-1",
		OperationType: "upsert",
		Payload:       `{"id":"product-1","name":"Keyboard"}`,
	}

	if err := worker.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent returned error: %v", err)
	}

	if len(search.upsertPayloads) != 1 {
		t.Fatalf("upsert calls = %d, want 1", len(search.upsertPayloads))
	}

	if got := string(search.upsertPayloads[0]); got != event.Payload {
		t.Fatalf("upsert payload = %s, want %s", got, event.Payload)
	}
}

func TestProcessEventDeleteUsesRecordID(t *testing.T) {
	search := &fakeSearchClient{}
	worker := &Worker{search: search}
	event := Event{RecordID: "product-1", OperationType: "delete"}

	if err := worker.processEvent(context.Background(), event); err != nil {
		t.Fatalf("processEvent returned error: %v", err)
	}

	if len(search.deletedIDs) != 1 {
		t.Fatalf("delete calls = %d, want 1", len(search.deletedIDs))
	}

	if got := search.deletedIDs[0]; got != event.RecordID {
		t.Fatalf("deleted id = %s, want %s", got, event.RecordID)
	}
}

func TestProcessEventPropagatesSearchFailure(t *testing.T) {
	wantErr := errors.New("typesense unavailable")
	search := &fakeSearchClient{upsertErr: wantErr}
	worker := &Worker{search: search}
	event := Event{OperationType: "upsert", Payload: `{"id":"product-1"}`}

	if err := worker.processEvent(context.Background(), event); !errors.Is(err, wantErr) {
		t.Fatalf("processEvent error = %v, want %v", err, wantErr)
	}
}

func TestProcessEventRejectsUnknownOperation(t *testing.T) {
	worker := &Worker{search: &fakeSearchClient{}}
	event := Event{OperationType: "archive"}

	if err := worker.processEvent(context.Background(), event); err == nil {
		t.Fatal("processEvent returned nil, want error")
	}
}
